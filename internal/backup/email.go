package backup

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yujixinjiang/backend/internal/config"

	zip "github.com/yeka/zip"
)

const (
	maxEmailAttachmentBytes = 4 * 1024 * 1024
	emailSentMarkerName     = ".last_backup_email_sent"
)

type emailAttachment struct {
	FileName      string `json:"file_name"`
	ContentBase64 string `json:"content_base64"`
}

type emailSendRequest struct {
	Email       string            `json:"email"`
	Purpose     string            `json:"purpose"`
	Subject     string            `json:"subject,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	ExternalRef string            `json:"external_ref,omitempty"`
	Attachments []emailAttachment `json:"attachments"`
}

type emailSendResponse struct {
	Message         string `json:"message"`
	EmailSent       bool   `json:"email_sent"`
	MessageID       string `json:"message_id"`
	AttachmentCount int    `json:"attachment_count"`
	ExternalRef     string `json:"external_ref"`
}

func (s *Scheduler) maybeEmailBackup(backupPath string) {
	cfg := s.cfg
	if !cfg.EmailEnabled {
		return
	}
	if err := validateEmailConfig(cfg); err != nil {
		log.Printf("[backup-email] 配置无效，跳过发信: %v", err)
		return
	}
	due, err := emailDue(cfg.Dir, cfg.EmailInterval)
	if err != nil {
		log.Printf("[backup-email] 读取发信记录失败: %v", err)
		return
	}
	if !due {
		log.Printf("[backup-email] 未到发信间隔（%s），跳过", cfg.EmailInterval)
		return
	}

	zipPath, err := makeEncryptedZip(backupPath, cfg.EmailZipPassword)
	if err != nil {
		log.Printf("[backup-email] 加密压缩失败: %v", err)
		return
	}
	defer os.Remove(zipPath)

	info, err := os.Stat(zipPath)
	if err != nil {
		log.Printf("[backup-email] 读取附件失败: %v", err)
		return
	}
	if info.Size() > maxEmailAttachmentBytes {
		log.Printf("[backup-email] 加密包过大（%d bytes > %d），跳过发信；本地备份仍保留: %s",
			info.Size(), maxEmailAttachmentBytes, backupPath)
		// 记入发信间隔，避免每个备份周期无限重试压缩/发信
		if err := markEmailSent(cfg.Dir, time.Now()); err != nil {
			log.Printf("[backup-email] 写入发信记录失败（超大附件跳过）: %v", err)
		}
		return
	}

	raw, err := os.ReadFile(zipPath)
	if err != nil {
		log.Printf("[backup-email] 读取加密包失败: %v", err)
		return
	}

	stamp := time.Now().Format("2006-01-02")
	zipName := filepath.Base(zipPath)
	subject := cfg.EmailSubject
	if subject == "" {
		subject = "雨季新江数据库备份"
	}
	subject = fmt.Sprintf("%s %s", subject, stamp)
	purpose := fmt.Sprintf(
		"雨季新江生产库定时备份（加密 ZIP）。附件：%s。请用备份密码解压；解压后为 .sql.gz，可用 gunzip 还原后导入 MySQL。",
		zipName,
	)
	ref := fmt.Sprintf("backup-%s", time.Now().Format("20060102_150405"))

	resp, err := sendAttachmentEmail(cfg, emailSendRequest{
		Email:       cfg.EmailTo,
		Purpose:     purpose,
		Subject:     subject,
		DisplayName: "雨季新江运维",
		ExternalRef: ref,
		Attachments: []emailAttachment{{
			FileName:      zipName,
			ContentBase64: base64.StdEncoding.EncodeToString(raw),
		}},
	})
	if err != nil {
		log.Printf("[backup-email] 发信失败: %v", err)
		return
	}
	if !resp.EmailSent {
		log.Printf("[backup-email] 接口返回未发送: message=%s id=%s", resp.Message, resp.MessageID)
		return
	}
	if err := markEmailSentWithRetry(cfg.Dir, time.Now(), 3); err != nil {
		log.Printf("[backup-email] 写入发信记录失败（已发送，可能重复投递）: %v", err)
		return
	}
	log.Printf("[backup-email] 已发送加密备份至邮箱 ref=%s message_id=%s", ref, resp.MessageID)
}

func validateEmailConfig(cfg config.BackupConfig) error {
	if cfg.EmailTo == "" {
		return fmt.Errorf("BACKUP_EMAIL_TO 未配置")
	}
	if cfg.EmailAPIKey == "" {
		return fmt.Errorf("BACKUP_EMAIL_API_KEY 未配置")
	}
	if cfg.EmailAPIURL == "" {
		return fmt.Errorf("BACKUP_EMAIL_API_URL 未配置")
	}
	if strings.TrimSpace(cfg.EmailZipPassword) == "" {
		return fmt.Errorf("BACKUP_EMAIL_ZIP_PASSWORD 未配置")
	}
	if len(cfg.EmailZipPassword) < 8 {
		return fmt.Errorf("BACKUP_EMAIL_ZIP_PASSWORD 过短（至少 8 位）")
	}
	return nil
}

func emailDue(dir string, interval time.Duration) (bool, error) {
	marker := filepath.Join(dir, emailSentMarkerName)
	raw, err := os.ReadFile(marker)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	ts := strings.TrimSpace(string(raw))
	if ts == "" {
		return true, nil
	}
	last, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return true, nil
	}
	return time.Since(last) >= interval, nil
}

func markEmailSent(dir string, at time.Time) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	marker := filepath.Join(dir, emailSentMarkerName)
	return os.WriteFile(marker, []byte(at.UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func markEmailSentWithRetry(dir string, at time.Time, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		if err := markEmailSent(dir, at); err != nil {
			last = err
			time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
			continue
		}
		return nil
	}
	return last
}

// makeEncryptedZip 将备份文件打成 AES-256 加密 ZIP（可用 7-Zip / WinRAR 解压）。
func makeEncryptedZip(srcPath, password string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	base := filepath.Base(srcPath)
	zipPath := filepath.Join(filepath.Dir(srcPath), strings.TrimSuffix(base, filepath.Ext(base)))
	if strings.HasSuffix(base, ".sql.gz") {
		zipPath = filepath.Join(filepath.Dir(srcPath), strings.TrimSuffix(base, ".sql.gz")+".zip")
	} else {
		zipPath = srcPath + ".zip"
	}

	out, err := os.OpenFile(zipPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	fw, err := zw.Encrypt(base, password, zip.AES256Encryption)
	if err != nil {
		zw.Close()
		out.Close()
		os.Remove(zipPath)
		return "", err
	}
	if _, err := io.Copy(fw, src); err != nil {
		zw.Close()
		out.Close()
		os.Remove(zipPath)
		return "", err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		os.Remove(zipPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(zipPath)
		return "", err
	}
	return zipPath, nil
}

func sendAttachmentEmail(cfg config.BackupConfig, req emailSendRequest) (*emailSendResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, cfg.EmailAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.EmailAPIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))

	if httpResp.StatusCode != http.StatusCreated && httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed emailSendResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w body=%s", err, strings.TrimSpace(string(respBody)))
	}
	return &parsed, nil
}
