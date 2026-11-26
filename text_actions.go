package main

import (
	"errors"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const maxMergeEntries = 20

func convertEntryCase(item Entry, mode string) (Entry, error) {
	if (item.Kind != "text" && item.Kind != "code") || item.Secret != "" {
		return Entry{}, errors.New("只能转换未遮罩的文字或代码")
	}
	var text string
	switch mode {
	case "upper":
		text = cases.Upper(language.Und).String(item.Text)
	case "lower":
		text = cases.Lower(language.Und).String(item.Text)
	default:
		return Entry{}, errors.New("无效的大小写转换")
	}
	return newEntry(text, nil, "")
}

func mergeTextEntries(items []Entry, separator string) (Entry, error) {
	if len(items) < 2 || len(items) > maxMergeEntries {
		return Entry{}, errors.New("请选择 2–20 条文字")
	}
	var join string
	switch separator {
	case "newline":
		join = "\n"
	case "blank":
		join = "\n\n"
	case "space":
		join = " "
	default:
		return Entry{}, errors.New("无效的分隔方式")
	}
	seen := make(map[string]bool, len(items))
	var text strings.Builder
	for index, item := range items {
		if item.Kind == "image" || item.Secret != "" || item.Text == "" || seen[item.ID] {
			return Entry{}, errors.New("请选择不同的未遮罩文字条目")
		}
		seen[item.ID] = true
		if index > 0 {
			text.WriteString(join)
		}
		if len(item.Text) > maxText-text.Len() {
			return Entry{}, errors.New("合并后的文字不能超过 512 KB")
		}
		text.WriteString(item.Text)
	}
	return newEntry(text.String(), nil, "")
}

func (d *Desktop) ConvertCase(session uint64, id, mode string, copyOnly bool) (PasteResult, error) {
	item, err := d.Detail(session, id)
	if err != nil {
		return PasteResult{}, err
	}
	converted, err := convertEntryCase(item, mode)
	if err != nil {
		return PasteResult{}, err
	}
	return d.deliver(converted, copyOnly, session)
}

func (d *Desktop) MergeText(session uint64, ids []string, separator string, copyOnly bool) (PasteResult, error) {
	if len(ids) < 2 || len(ids) > maxMergeEntries {
		return PasteResult{}, errors.New("请选择 2–20 条文字")
	}
	items := make([]Entry, 0, len(ids))
	for _, id := range ids {
		item, err := d.Detail(session, id)
		if err != nil {
			return PasteResult{}, err
		}
		if item.Kind == "image" || item.Secret != "" {
			return PasteResult{}, errors.New("请选择未遮罩的文字条目")
		}
		items = append(items, item)
	}
	merged, err := mergeTextEntries(items, separator)
	if err != nil {
		return PasteResult{}, err
	}
	return d.deliver(merged, copyOnly, session)
}
