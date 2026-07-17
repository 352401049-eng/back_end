package backup

import (
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"yujixinjiang/backend/internal/config"
)

// Scheduler 按间隔在后台 goroutine 中执行 mysqldump。
type Scheduler struct {
	db     config.DBConfig
	cfg    config.BackupConfig
	cancel context.CancelFunc
}

func NewScheduler(db config.DBConfig, cfg config.BackupConfig) *Scheduler {
	return &Scheduler{db: db, cfg: cfg}
}

// Start 启动定时备份；ctx 取消时停止。
func (s *Scheduler) Start(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o750); err != nil {
		log.Printf("[backup] 创建备份目录失败: %v", err)
		return
	}

	child, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.loop(child)
	log.Printf("[backup] 已启动定时备份: 间隔=%s 目录=%s 保留=%d天", s.cfg.Interval, s.cfg.Dir, s.cfg.RetainDays)
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	s.runOnce()
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce()
		}
	}
}

func (s *Scheduler) runOnce() {
	path, err := Dump(s.db, s.cfg)
	if err != nil {
		log.Printf("[backup] 备份失败: %v", err)
		return
	}
	log.Printf("[backup] 备份完成: %s", path)
	if err := cleanup(s.cfg.Dir, s.cfg.RetainDays); err != nil {
		log.Printf("[backup] 清理旧备份失败: %v", err)
	}
}

// Dump 执行一次 mysqldump，返回备份文件路径。
func Dump(db config.DBConfig, cfg config.BackupConfig) (string, error) {
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return "", err
	}

	stamp := time.Now().Format("20060102_150405")
	rawPath := filepath.Join(cfg.Dir, fmt.Sprintf("%s_%s.sql", db.Name, stamp))

	args := []string{
		"-h", db.Host,
		"-P", db.Port,
		"-u", db.User,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--set-gtid-purged=OFF",
		"--default-character-set=utf8mb4",
		db.Name,
	}

	cmd := exec.Command("mysqldump", args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+db.Password)

	if cfg.Compress {
		finalPath := rawPath + ".gz"
		gzFile, err := os.Create(finalPath)
		if err != nil {
			return "", fmt.Errorf("创建备份文件失败: %w", err)
		}
		gw := gzip.NewWriter(gzFile)
		cmd.Stdout = gw
		if err := cmd.Run(); err != nil {
			gw.Close()
			gzFile.Close()
			os.Remove(finalPath)
			return "", fmt.Errorf("mysqldump 执行失败（请确认已安装 mysql-client）: %w", err)
		}
		if err := gw.Close(); err != nil {
			gzFile.Close()
			os.Remove(finalPath)
			return "", err
		}
		if err := gzFile.Close(); err != nil {
			os.Remove(finalPath)
			return "", err
		}
		return finalPath, nil
	}

	out, err := os.Create(rawPath)
	if err != nil {
		return "", fmt.Errorf("创建备份文件失败: %w", err)
	}
	cmd.Stdout = out
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		os.Remove(rawPath)
		return "", fmt.Errorf("mysqldump 执行失败（请确认已安装 mysql-client）: %w", runErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	return rawPath, nil
}

func cleanup(dir string, retainDays int) error {
	if retainDays <= 0 {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") && !strings.HasSuffix(name, ".sql.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				log.Printf("[backup] 删除过期备份 %s 失败: %v", name, err)
			}
		}
	}
	return nil
}

// ListFiles 返回目录内备份文件（新→旧）。
func ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".sql.gz") {
			files = append(files, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files, nil
}
