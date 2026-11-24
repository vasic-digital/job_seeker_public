package ext

import (
	"context"
	"fmt"
)

// ReaderCase is one golden fixture for a StandardReader.
type ReaderCase struct {
	Name        string
	ContentType string
	Raw         []byte
	WantOK      bool                 // the document is this reader's: ok=true
	WantErr     bool                 // malformed: a non-nil error (a silent skip would hide a broken source)
	Check       func(Posting) string // returns "" when the Posting is as expected, else the complaint
}

// ConformReader is the per-plugin suite every StandardReader must pass: golden cases, determinism, Sniff agreement
// with ok, "not mine" is (ok=false, err=nil), and no panic on hostile input.
func ConformReader(t Reporter, r StandardReader, cases []ReaderCase) {
	t.Helper()
	if r.Info().Kind != "standard_reader" || r.Info().Version == "" {
		t.Fatalf("bad Info %+v", r.Info())
	}
	for _, c := range cases {
		p1, ok1, err1 := safeRead(r, c.Raw)
		p2, ok2, err2 := safeRead(r, c.Raw)
		if ok1 != ok2 || (err1 == nil) != (err2 == nil) || fmt.Sprint(p1) != fmt.Sprint(p2) {
			t.Errorf("%s: not deterministic", c.Name)
		}
		if ok1 != c.WantOK {
			t.Errorf("%s: ok=%v want %v", c.Name, ok1, c.WantOK)
		}
		if (err1 != nil) != c.WantErr {
			t.Errorf("%s: err=%v want error=%v", c.Name, err1, c.WantErr)
		}
		if !ok1 && err1 == nil && p1.Title != "" {
			t.Errorf("%s: a not-mine document returned a Posting", c.Name)
		}
		if c.WantOK && !r.Sniff(c.ContentType, head(c.Raw)) {
			t.Errorf("%s: Sniff rejects a document the reader accepts", c.Name)
		}
		if ok1 && c.Check != nil {
			if msg := c.Check(p1); msg != "" {
				t.Errorf("%s: %s", c.Name, msg)
			}
		}
	}
	for i, hostile := range [][]byte{nil, {}, []byte("\x00\xff\xfe"), []byte("{"), []byte("["), []byte("<"), []byte(`{"@type":`), make([]byte, 1<<16)} {
		if _, _, _, panicked := tryRead(r, hostile); panicked {
			t.Errorf("hostile input %d panicked the reader", i)
		}
	}
}

func head(b []byte) []byte {
	if len(b) > 512 {
		return b[:512]
	}
	return b
}

func safeRead(r StandardReader, raw []byte) (p Posting, ok bool, err error) {
	p, ok, err, _ = tryRead(r, raw)
	return
}

func tryRead(r StandardReader, raw []byte) (p Posting, ok bool, err error, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	p, ok, err = r.Read(context.Background(), raw)
	return
}
