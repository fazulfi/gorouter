package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactAndPITR(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.dump")
	if e := os.WriteFile(p, []byte("payload"), 0600); e != nil {
		t.Fatal(e)
	}
	a, e := Capture(p, "full", "", time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.Verify(); e != nil {
		t.Fatal(e)
	}
	r := CommandRunnerFunc(func(_ context.Context, n string, args, env []string) ([]byte, []byte, error) {
		if n != "pg_restore" {
			t.Fatal(n)
		}
		return nil, nil, nil
	})
	if e = Restore(context.Background(), r, a, "target", &PITRPoint{WALPath: "archive", LSN: "0/10", At: time.Now()}); e != nil {
		t.Fatal(e)
	}
}
func TestRetention(t *testing.T) {
	d := t.TempDir()
	now := time.Now()
	var as []Artifact
	for i := 0; i < 3; i++ {
		p := filepath.Join(d, string(rune('a'+i)))
		os.WriteFile(p, []byte{byte(i)}, 0600)
		a, _ := Capture(p, "full", "", now.Add(-time.Duration(i)*time.Hour))
		as = append(as, a)
	}
	kept, pruned, e := Retain(as, 2, now, 0)
	if e != nil || len(kept) != 2 || len(pruned) != 1 {
		t.Fatalf("%d %d %v", len(kept), len(pruned), e)
	}
}
func TestRestoreRejectsInvalidPITR(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x")
	_ = os.WriteFile(p, []byte("x"), 0600)
	a, _ := Capture(p, "full", "", time.Now())
	err := Restore(context.Background(), CommandRunnerFunc(func(context.Context, string, []string, []string) ([]byte, []byte, error) { return nil, nil, nil }), a, "db", &PITRPoint{WALPath: ""})
	if err == nil {
		t.Fatal("invalid PITR point must fail")
	}
}

func TestRestorePropagatesRunnerFailure(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x")
	_ = os.WriteFile(p, []byte("x"), 0600)
	a, _ := Capture(p, "full", "", time.Now())
	err := Restore(context.Background(), CommandRunnerFunc(func(context.Context, string, []string, []string) ([]byte, []byte, error) {
		return nil, []byte("failed"), os.ErrPermission
	}), a, "db", nil)
	if err == nil {
		t.Fatal("runner failure must propagate")
	}
}

func TestDelete(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x")
	if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Delete(d, "x"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(d, ""); err == nil {
		t.Fatal("empty path must fail")
	}
}

func TestDeleteRejectsEscapes(t *testing.T) {
	d := t.TempDir()
	outside := filepath.Join(filepath.Dir(d), "outside-backup-test")
	_ = os.WriteFile(outside, []byte("x"), 0600)
	defer os.Remove(outside)
	for _, name := range []string{"../outside-backup-test", filepath.Join(d, "absolute")} {
		if err := Delete(d, name); err == nil {
			t.Fatalf("unsafe path %q must fail", name)
		}
	}
}

func TestRetentionPreservesNewestWhenAllExpired(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "only")
	_ = os.WriteFile(p, []byte("old"), 0600)
	a, _ := Capture(p, "full", "", time.Now().Add(-48*time.Hour))
	kept, pruned, err := Retain([]Artifact{a}, 1, time.Now(), 24*time.Hour)
	if err != nil || len(kept) != 1 || len(pruned) != 0 {
		t.Fatalf("kept=%d pruned=%d err=%v", len(kept), len(pruned), err)
	}
}

func TestRestoreRejectsUnsafeInputs(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x")
	_ = os.WriteFile(p, []byte("x"), 0600)
	a, _ := Capture(p, "full", "", time.Now())
	r := CommandRunnerFunc(func(context.Context, string, []string, []string) ([]byte, []byte, error) {
		t.Fatal("runner must not run")
		return nil, nil, nil
	})
	cases := []struct {
		target string
		point  *PITRPoint
	}{
		{"../db", nil}, {"db;drop", nil}, {"db", &PITRPoint{WALPath: "../wal", LSN: "0/10", At: time.Now()}},
		{"db", &PITRPoint{WALPath: "archive", LSN: "not-lsn", At: time.Now()}},
		{"db", &PITRPoint{WALPath: "C:wal", LSN: "0/10", At: time.Now()}},
		{"db", &PITRPoint{WALPath: "archive:wal", LSN: "0/10", At: time.Now()}},
		{"db", &PITRPoint{WALPath: `C:\\wal`, LSN: "0/10", At: time.Now()}},
		{"db", &PITRPoint{WALPath: `archive\\wal:00000001`, LSN: "0/10", At: time.Now()}},
	}
	for _, tc := range cases {
		if err := Restore(context.Background(), r, a, tc.target, tc.point); err == nil {
			t.Fatalf("unsafe input accepted: %+v", tc)
		}
	}
}
func TestRetentionRejectsOld(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "old")
	_ = os.WriteFile(p, []byte("old"), 0600)
	a, _ := Capture(p, "full", "", time.Now().Add(-48*time.Hour))
	kept, pruned, err := Retain([]Artifact{a}, 1, time.Now(), 24*time.Hour)
	if err != nil || len(kept) != 1 || len(pruned) != 0 {
		t.Fatalf("kept=%d pruned=%d err=%v", len(kept), len(pruned), err)
	}
}
func TestCorruptFailsClosed(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x")
	os.WriteFile(p, []byte("x"), 0600)
	a, _ := Capture(p, "full", "", time.Now())
	os.WriteFile(p, []byte("tamper"), 0600)
	if e := a.Verify(); e == nil {
		t.Fatal("expected corruption")
	}
}
