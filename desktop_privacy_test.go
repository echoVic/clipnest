package main

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
)

func textCopy(revision int64, text string) *testClipboard {
	return &testClipboard{access: clipboardAllowed, revision: revision, portable: []transfer.Format{transfer.Text}, text: []byte(text)}
}

func clipboardPage(t *testing.T, d *Desktop) []Entry {
	t.Helper()
	v, err := d.h.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	page, err := v.Page("", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	return page.Entries
}

func setPrivacy(t *testing.T, d *Desktop, change func(*PrivacySettings)) {
	t.Helper()
	s := d.storage.Privacy.Settings()
	change(&s)
	if err := d.storage.Privacy.Save(s); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureMasksSecretsByDefault(t *testing.T) {
	d := clipboardDesktop(t)
	text := "AWS_SECRET=" + testAWSKey
	if captured, err := d.captureFrom(textCopy(1, text), false); !captured || err != nil {
		t.Fatal(captured, err)
	}
	entries := clipboardPage(t, d)
	if len(entries) != 1 || entries[0].Secret != "AWS 访问密钥" || entries[0].Title != "AWS_SECRET=AKIA••••MPLE" {
		t.Fatal("secret not masked", entries)
	}
	v, _ := d.h.OpenView()
	defer v.Close()
	if detail, err := v.Detail(entries[0].ID); err != nil || detail.Text != text {
		t.Fatal("the original is what gets pasted", detail, err)
	}
}

func TestCaptureSecretModes(t *testing.T) {
	for mode, want := range map[string]string{secretsSkip: "none", secretsOff: "plain"} {
		t.Run(mode, func(t *testing.T) {
			d := clipboardDesktop(t)
			setPrivacy(t, d, func(s *PrivacySettings) { s.Secrets = mode })
			captured, err := d.captureFrom(textCopy(1, "token "+testGitHub), false)
			entries := clipboardPage(t, d)
			switch want {
			case "none":
				if captured || err != nil || len(entries) != 0 {
					t.Fatal("skip mode recorded a secret", captured, err, entries)
				}
			case "plain":
				if !captured || err != nil || len(entries) != 1 || entries[0].Secret != "" || entries[0].Title != "token "+testGitHub {
					t.Fatal("off mode changed the copy", captured, err, entries)
				}
			}
			if captured, err := d.captureFrom(textCopy(2, "ordinary words"), false); !captured || err != nil {
				t.Fatal("ordinary text not recorded", captured, err)
			}
		})
	}
}

func TestCaptureIgnoreRules(t *testing.T) {
	d := clipboardDesktop(t)
	setPrivacy(t, d, func(s *PrivacySettings) { s.Ignore = []string{`^\d{6}$`, `(?i)confidential`} })
	for i, text := range []string{"123456", "Marked CONFIDENTIAL\nbody"} {
		if captured, err := d.captureFrom(textCopy(int64(i+1), text), false); captured || err != nil {
			t.Fatalf("ignored %q was recorded", text)
		}
	}
	if captured, err := d.captureFrom(textCopy(3, "1234567"), false); !captured || err != nil {
		t.Fatal("unmatched text not recorded", captured, err)
	}
	setPrivacy(t, d, func(s *PrivacySettings) { s.Ignore = []string{"."} })
	var data bytes.Buffer
	png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	c := &testClipboard{access: clipboardAllowed, revision: 4, portable: []transfer.Format{transfer.PNG}, image: data.Bytes()}
	if captured, err := d.captureFrom(c, false); !captured || err != nil {
		t.Fatal("ignore rules apply to text only", captured, err)
	}
}

func cleared(revision int64) *testClipboard {
	return &testClipboard{access: clipboardAllowed, revision: revision}
}

func TestClearSyncForgetsTheCopyBeforeIt(t *testing.T) {
	for name, clear := range map[string]*testClipboard{
		"no formats":      cleared(2),
		"empty text":      textCopy(2, ""),
		"whitespace text": textCopy(2, " \n\t"),
	} {
		t.Run(name, func(t *testing.T) {
			d := clipboardDesktop(t)
			d.captureFrom(textCopy(1, "earlier"), false)
			d.captureFrom(textCopy(2, "hunter2"), false)
			clear.revision = 3
			if captured, err := d.captureFrom(clear, false); captured || err != nil {
				t.Fatal(captured, err)
			}
			if entries := clipboardPage(t, d); len(entries) != 1 || entries[0].Title != "earlier" {
				t.Fatal("clear did not forget only the last copy", entries)
			}
			if captured, _ := d.captureFrom(cleared(4), false); captured || len(clipboardPage(t, d)) != 1 {
				t.Fatal("a second clear forgot an older copy")
			}
		})
	}
}

func TestClearSyncFromAnExcludedApp(t *testing.T) {
	d := clipboardDesktop(t)
	d.storage.Exclusions.Add(ExcludedApp{"com.private", "Private"})
	c := textCopy(1, "hunter2")
	c.source = targetApp{Name: "Browser", BundleID: "com.browser"}
	d.captureFrom(c, false)
	clear := cleared(2)
	clear.source = targetApp{Name: "Private", BundleID: "com.private"}
	d.captureFrom(clear, false)
	if entries := clipboardPage(t, d); len(entries) != 0 {
		t.Fatal("clear while an excluded app is in front kept the copy", entries)
	}
}

func TestClearSyncKeepsTheCopy(t *testing.T) {
	var data bytes.Buffer
	png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	for name, between := range map[string]func(*testing.T, *Desktop){
		"turned off": func(t *testing.T, d *Desktop) {
			setPrivacy(t, d, func(s *PrivacySettings) { s.ClearSync = false })
		},
		"after five minutes": func(t *testing.T, d *Desktop) { d.recent.at = time.Now().Add(-clearSyncWindow - time.Second) },
		"another change first": func(t *testing.T, d *Desktop) {
			c := textCopy(2, "")
			c.portable = append(c.portable, transfer.FileList)
			d.captureFrom(c, false)
		},
		"an ignored copy first": func(t *testing.T, d *Desktop) {
			setPrivacy(t, d, func(s *PrivacySettings) { s.Ignore = []string{"^skip me$"} })
			d.captureFrom(textCopy(2, "skip me"), false)
		},
		"pasted from Clipnest": func(t *testing.T, d *Desktop) {
			d.lastRevision, d.lastID = 2, "pasted" // what deliver records after writing
		},
		"paused": func(t *testing.T, d *Desktop) {
			d.h.Pause(true)
			d.captureFrom(cleared(2), false)
			d.h.Pause(false)
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := clipboardDesktop(t)
			d.captureFrom(textCopy(1, "hunter2"), false)
			between(t, d)
			d.captureFrom(cleared(3), false)
			if entries := clipboardPage(t, d); len(entries) != 1 || entries[0].Title != "hunter2" {
				t.Fatal("clear removed the copy", entries)
			}
		})
	}
	t.Run("no revision", func(t *testing.T) {
		d := clipboardDesktop(t)
		d.captureFrom(textCopy(-1, "hunter2"), false)
		d.captureFrom(cleared(-1), false)
		if len(clipboardPage(t, d)) != 1 {
			t.Fatal("a clear without revisions removed the copy")
		}
	})
	t.Run("image then clear", func(t *testing.T) {
		d := clipboardDesktop(t)
		d.captureFrom(&testClipboard{access: clipboardAllowed, revision: 1, portable: []transfer.Format{transfer.PNG}, image: data.Bytes()}, false)
		d.captureFrom(cleared(2), false)
		if entries := clipboardPage(t, d); len(entries) != 0 {
			t.Fatal("an image is forgotten like text", entries)
		}
	})
}

func TestClearSyncWindowIsFiveMinutes(t *testing.T) {
	if clearSyncWindow != 5*time.Minute {
		t.Fatal(clearSyncWindow)
	}
	d := clipboardDesktop(t)
	d.captureFrom(textCopy(1, "hunter2"), false)
	d.recent.at = time.Now().Add(-clearSyncWindow + time.Second)
	d.captureFrom(cleared(2), false)
	if entries := clipboardPage(t, d); len(entries) != 0 {
		t.Fatal("clear inside the window kept the copy", entries)
	}
}
