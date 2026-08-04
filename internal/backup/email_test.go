package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEmailDue(t *testing.T) {
	dir := t.TempDir()
	due, err := emailDue(dir, time.Hour)
	if err != nil || !due {
		t.Fatalf("empty marker should be due: due=%v err=%v", due, err)
	}
	if err := markEmailSent(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	due, err = emailDue(dir, time.Hour)
	if err != nil || due {
		t.Fatalf("just sent should not be due: due=%v err=%v", due, err)
	}
	if err := markEmailSent(dir, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	due, err = emailDue(dir, time.Hour)
	if err != nil || !due {
		t.Fatalf("stale marker should be due: due=%v err=%v", due, err)
	}
}

func TestMakeEncryptedZip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "demo_20260101_120000.sql.gz")
	if err := os.WriteFile(src, []byte("hello-backup-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	zipPath, err := makeEncryptedZip(src, "test-pass-123")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(zipPath)
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 1 {
		t.Fatal("empty zip")
	}
	if filepath.Ext(zipPath) != ".zip" {
		t.Fatalf("ext=%s", filepath.Ext(zipPath))
	}
}

func TestMaskEmail(t *testing.T) {
	got := maskEmail("352401049@qq.com")
	if got == "352401049@qq.com" || got == "" {
		t.Fatalf("mask failed: %s", got)
	}
}
