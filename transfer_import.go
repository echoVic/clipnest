package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"runtime"
	"unicode/utf8"
)

// importFile is an export file open for reading. Only the names of the
// format are looked at; no other file is read.
type importFile struct {
	zr       *zip.ReadCloser
	files    map[string]*zip.File
	manifest exportManifest
}

// openImportFile opens path and checks its manifest. A file that isn't an
// export, comes from a newer Clipnest or is damaged is refused.
func openImportFile(path string) (*importFile, error) {
	zr, err := zip.OpenReader(path)
	if errors.Is(err, zip.ErrInsecurePath) {
		err = nil // nothing is extracted, and such names are never read
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, errors.New("导入文件不见了，可能已被移动或删除")
	case errors.Is(err, fs.ErrPermission):
		return nil, errors.New("没有权限读取导入文件")
	case err != nil:
		return nil, errNotExport
	}
	f := &importFile{zr: zr, files: map[string]*zip.File{}}
	if err := f.index(); err != nil {
		zr.Close()
		return nil, err
	}
	return f, nil
}

func (f *importFile) Close() error { return f.zr.Close() }

func (f *importFile) index() error {
	if len(f.zr.File) > maxRetainedEntries+maxExtraFiles {
		return damaged("文件数量超过上限")
	}
	for _, zf := range f.zr.File {
		name := zf.Name
		if name != manifestName && name != historyName && name != phrasesName && name != settingsName && !validImageName(name) {
			continue
		}
		if f.files[name] != nil {
			return damaged(name + " 出现了不止一次")
		}
		f.files[name] = zf
	}
	if f.files[manifestName] == nil {
		return errNotExport
	}
	data, err := readZipFile(f.files[manifestName], maxManifestBytes)
	if err == nil {
		err = decodeExportJSON(data, &f.manifest)
	}
	if err != nil {
		return damaged("manifest.json：" + err.Error())
	}
	switch {
	case f.manifest.Format != exportFormat:
		return errNotExport
	case f.manifest.Version > exportVersion:
		return errNewerExport
	case f.manifest.Version < 1:
		return damaged("版本无效")
	case f.files[historyName] == nil || f.files[phrasesName] == nil:
		return damaged("缺少 history.json 或 phrases.json")
	}
	return nil
}

var errTooLarge = errors.New("超过大小上限")

// readZipFile reads a file of the ZIP, at most limit bytes whatever its
// header says. Reading to the end also checks its checksum.
func readZipFile(zf *zip.File, limit int64) ([]byte, error) {
	r, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

// decodeExportJSON decodes exactly one document. encoding/json would
// otherwise replace invalid UTF-8 with U+FFFD.
func decodeExportJSON(data []byte, v any) error {
	if !utf8.Valid(data) {
		return errors.New("包含无效的 UTF-8")
	}
	return json.Unmarshal(data, v)
}

var errItemTooLarge = errors.New("单条历史超过 4 MB")

// limitedReader fails a read past limit, so one element can't grow the
// decoder's buffer without bound.
type limitedReader struct {
	r        io.Reader
	n, limit int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n >= l.limit {
		return 0, errItemTooLarge
	}
	if int64(len(p)) > l.limit-l.n {
		p = p[:l.limit-l.n]
	}
	n, err := l.r.Read(p)
	l.n += int64(n)
	return n, err
}

// eachItem decodes history.json one element at a time and calls fn with
// each, ok false for an element that isn't an item. Broken JSON, an element
// over maxHistoryItemBytes or more than maxRetainedEntries elements make the
// file damaged; an error from fn stops and is returned.
func (f *importFile) eachItem(fn func(item exportItem, ok bool) error) error {
	r, err := f.files[historyName].Open()
	if err != nil {
		return damaged("history.json：" + err.Error())
	}
	defer r.Close()
	bad := func(err error) error { return damaged("history.json：" + err.Error()) }
	in := &limitedReader{r: r, limit: maxHistoryItemBytes}
	dec := json.NewDecoder(in)
	if tok, err := dec.Token(); err != nil {
		return bad(err)
	} else if tok != json.Delim('[') {
		return bad(errors.New("不是数组"))
	}
	for n := 0; ; n++ {
		// Each element, and the space before it, gets the same allowance.
		in.limit = dec.InputOffset() + maxHistoryItemBytes
		if !dec.More() {
			break
		}
		if n == maxRetainedEntries {
			return bad(errors.New("超过 100 万条"))
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return bad(err)
		}
		var item exportItem
		ok := json.Unmarshal(raw, &item) == nil
		if ok && !utf8.Valid(raw) {
			// Invalid UTF-8 makes the item invalid, except in its source,
			// which is cleared. Unmarshal replaced the bad bytes, so look
			// at the raw text and image.
			var fields struct{ Text, Image json.RawMessage }
			ok = json.Unmarshal(raw, &fields) == nil && utf8.Valid(fields.Text) && utf8.Valid(fields.Image)
			item.Source = ""
		}
		if err := fn(item, ok); err != nil {
			return err
		}
	}
	if tok, err := dec.Token(); err != nil {
		return bad(err)
	} else if tok != json.Delim(']') {
		return bad(errors.New("不是数组"))
	}
	if _, err := dec.Token(); err != io.EOF {
		return bad(errors.New("末尾有多余的内容"))
	}
	return nil
}

// entry checks an item the way a copy is checked and builds its entry. A
// source that isn't valid UTF-8 or is too long is dropped; a time in the
// future becomes now.
func (f *importFile) entry(item exportItem, now int64) (Entry, error) {
	if (item.Text == "") == (item.Image == "") {
		return Entry{}, errors.New("条目要么是文字，要么是图片")
	}
	if item.Created <= 0 {
		return Entry{}, errors.New("时间无效")
	}
	source := item.Source
	if len(source) > maxSourceBytes || !utf8.ValidString(source) {
		source = ""
	}
	var png []byte
	if item.Image != "" {
		zf := f.files[item.Image]
		if !validImageName(item.Image) || zf == nil {
			return Entry{}, errors.New("图片不在文件里")
		}
		var err error
		if png, err = readZipFile(zf, maxImage); err != nil {
			return Entry{}, err
		}
	}
	e, err := newEntry(item.Text, png, source)
	if err != nil {
		return Entry{}, err
	}
	e.Created = min(item.Created, now)
	e.EntryPinned = item.EntryPinned
	return e, nil
}

// boundedPhrases decodes the phrase array element by element and stops at
// the first one past maxPhrases, so a file of tiny phrases can't allocate
// more than the cap.
type boundedPhrases []string

var errTooManyPhrases = errors.New("常用语超过 500 条")

func (p *boundedPhrases) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil {
		return err
	} else if tok == nil {
		return nil // null is an empty list
	} else if tok != json.Delim('[') {
		return errors.New("常用语不是数组")
	}
	list := []string{}
	for dec.More() {
		if len(list) == maxPhrases {
			return errTooManyPhrases
		}
		var s string
		if err := dec.Decode(&s); err != nil {
			return err
		}
		list = append(list, s)
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*p = list
	return nil
}

// phrases reads the file's phrase texts. Each text is checked when it is
// added; more than maxPhrases make the file damaged, without decoding the
// rest.
func (f *importFile) phrases() ([]string, error) {
	data, err := readZipFile(f.files[phrasesName], maxPhrasesBytes)
	var p struct {
		Phrases boundedPhrases `json:"phrases"`
	}
	if err == nil {
		err = decodeExportJSON(data, &p)
	}
	if err != nil {
		return nil, damaged("phrases.json：" + err.Error())
	}
	return p.Phrases, nil
}

// settings reads the file's settings, nil when it has none. check says
// whether they can be imported.
func (f *importFile) settings() (*exportSettings, error) {
	zf := f.files[settingsName]
	if zf == nil {
		return nil, nil
	}
	data, err := readZipFile(zf, maxSettingsBytes)
	var s exportSettings
	if err == nil {
		err = decodeExportJSON(data, &s)
	}
	if err != nil {
		return nil, damaged("settings.json：" + err.Error())
	}
	s.Privacy = s.Privacy.clone() // a missing ignore list is an empty one
	return &s, nil
}

// check says why the settings can't be imported here. Another platform's
// shortcut and excluded apps aren't imported, so they aren't checked.
func (s *exportSettings) check() error {
	if err := s.Retention.validate(); err != nil {
		return err
	}
	if _, err := s.Privacy.compile(); err != nil {
		return err
	}
	if s.Platform == "" {
		return errors.New("没有写明来自哪个平台")
	}
	if s.Platform != runtime.GOOS {
		return nil
	}
	if s.Shortcut != "" {
		// A common combination an older version saved is left out on
		// import, not a damaged file.
		if _, err := parseShortcutBinding(runtime.GOOS, s.Shortcut); err != nil && !errors.As(err, new(commonShortcutError)) {
			return err
		}
	}
	_, err := checkExcludedApps(s.ExcludedApps)
	return err
}

type itemOutcome int

const (
	itemKept itemOutcome = iota
	itemInvalid
	itemSkipped // by this computer's privacy settings
)

// importedEntry builds the entry an item imports as: checked the way a copy
// is, then judged and masked by this computer's privacy settings the way
// capture judges a copy.
func (f *importFile) importedEntry(item exportItem, ok bool, now int64, privacy *privacyStore) (Entry, itemOutcome) {
	if !ok {
		return Entry{}, itemInvalid
	}
	e, err := f.entry(item, now)
	if err != nil {
		return Entry{}, itemInvalid
	}
	if !privacy.apply(&e) {
		return Entry{}, itemSkipped
	}
	return e, itemKept
}
