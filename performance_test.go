package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// The literal pre-change query is the semantic oracle. It deliberately keeps
// whitespace, order, substring matching across fields and empty-slice shape.
func referenceQuery(entries []Entry, q, kind string) []Entry {
	out := []Entry{}
	q = strings.ToLower(strings.TrimSpace(q))
	for _, e := range entries {
		if kind != "all" && kind != "" && e.Kind != kind {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Text+" "+e.Title+" "+e.Source), q) {
			continue
		}
		out = append(out, e.summary())
	}
	return out
}

func benchmarkEntries(n, length int) []Entry {
	out := make([]Entry, n)
	for i := range out {
		text := fmt.Sprintf("Entry %d \n", i) + strings.Repeat("HELLO 中文 Clipboard ", length/23)
		e, _ := newEntry(text, nil, "Editor")
		out[i] = e
	}
	return out
}

func BenchmarkReferenceQuery(b *testing.B) {
	for _, length := range []int{256, 128 << 10} {
		b.Run(fmt.Sprint(length), func(b *testing.B) {
			entries := benchmarkEntries(maxItems, length)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = referenceQuery(entries, "clipboard", "all")
			}
		})
	}
}

func BenchmarkCachedQuery(b *testing.B) {
	for _, length := range []int{256, 128 << 10} {
		b.Run(fmt.Sprint(length), func(b *testing.B) {
			entries := benchmarkEntries(maxItems, length)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = queryEntries(entries, "clipboard", "all")
			}
		})
	}
}

func TestCachedQueryMatchesLiteralBaseline(t *testing.T) {
	entries := benchmarkEntries(5, 256)
	entries = append(entries, Entry{ID: "literal-image", Kind: "image", Title: "图像", Source: "Source", Image: []byte{1}}, Entry{ID: "literal-text", Kind: "text", Text: "中文\nUPPERCASE", Title: "end", Source: "Beginning"})
	for _, q := range []string{"", "  CLIPBOARD ", "中文", "uppercase", "end beginning", "unknown", "\n"} {
		for _, kind := range []string{"", "all", "text", "code", "image", "unknown"} {
			got, want := queryEntries(entries, q, kind), referenceQuery(entries, q, kind)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%q/%q: got %#v, want %#v", q, kind, got, want)
			}
		}
	}
	h := &History{}
	old, _ := newEntry("same payload", nil, "Original")
	next, _ := newEntry("same payload", nil, "Replacement")
	h.Add(old)
	frozen, _ := h.Snapshot()
	h.Add(next)
	latest, _ := h.Snapshot()
	if len(queryEntries(frozen, "original", "all")) != 1 || len(queryEntries(latest, "replacement", "all")) != 1 || len(queryEntries(latest, "original", "all")) != 0 {
		t.Fatal("source cache or frozen session became stale")
	}
}

type queryPair struct {
	ReferenceNS int64 `json:"reference_ns"`
	CandidateNS int64 `json:"candidate_ns"`
}
type queryMeasurement struct {
	Name              string      `json:"name"`
	Pairs             []queryPair `json:"pairs"`
	ReferenceMedianNS float64     `json:"reference_median_ns"`
	CandidateMedianNS float64     `json:"candidate_median_ns"`
	ImprovementLow    float64     `json:"improvement_low"`
	ImprovementHigh   float64     `json:"improvement_high"`
}

func medianNS(values []float64) float64 { sort.Float64s(values); return values[len(values)/2] }

// Manual opt-in, no wall-time assertions in ordinary CI. Preregistered gate:
// >=10% relative improvement with 95% paired bootstrap lower bound above 10%.
// This is an engineering acceptance gate, not a promised end-to-end latency.
func TestQueryPairedExperiment(t *testing.T) {
	path := os.Getenv("CLIPNEST_QUERY_PERF_OUTPUT")
	if path == "" {
		t.Skip("opt-in paired performance experiment")
	}
	measurements := []queryMeasurement{}
	for _, length := range []int{256, 128 << 10} {
		entries := benchmarkEntries(maxItems, length)
		for i := range entries {
			entries[i].Text += " EndNeedle"
			entries[i].searchText = prepareSearch(entries[i])
		}
		for _, q := range []string{"clipboard", "endneedle", "absentneedle"} {
			if !reflect.DeepEqual(referenceQuery(entries, q, "all"), queryEntries(entries, q, "all")) {
				t.Fatal("oracle failed")
			}
			m := queryMeasurement{Name: fmt.Sprintf("%d/%s", length, q)}
			measure := func(fn func([]Entry, string, string) []Entry) int64 {
				start := time.Now()
				for i := 0; i < 3; i++ {
					result := fn(entries, q, "all")
					if q != "absentneedle" && len(result) != maxItems {
						t.Fatal("invalid sample")
					}
				}
				return time.Since(start).Nanoseconds() / 3
			}
			_ = referenceQuery(entries, q, "all")
			_ = queryEntries(entries, q, "all")
			for i := 0; i < 15; i++ {
				p := queryPair{}
				if i%2 == 0 {
					p.ReferenceNS = measure(referenceQuery)
					p.CandidateNS = measure(queryEntries)
				} else {
					p.CandidateNS = measure(queryEntries)
					p.ReferenceNS = measure(referenceQuery)
				}
				m.Pairs = append(m.Pairs, p)
			}
			refs, cands, ratios := []float64{}, []float64{}, []float64{}
			for _, p := range m.Pairs {
				refs = append(refs, float64(p.ReferenceNS))
				cands = append(cands, float64(p.CandidateNS))
				ratios = append(ratios, 1-float64(p.CandidateNS)/float64(p.ReferenceNS))
			}
			m.ReferenceMedianNS = medianNS(refs)
			m.CandidateMedianNS = medianNS(cands)
			rng := rand.New(rand.NewSource(42))
			bootstrap := make([]float64, 4000)
			for i := range bootstrap {
				sample := make([]float64, len(ratios))
				for j := range sample {
					sample[j] = ratios[rng.Intn(len(ratios))]
				}
				bootstrap[i] = medianNS(sample)
			}
			sort.Float64s(bootstrap)
			m.ImprovementLow = bootstrap[100]
			m.ImprovementHigh = bootstrap[3899]
			if m.ImprovementLow <= 0.10 {
				t.Errorf("gate failed: %s %.3f", m.Name, m.ImprovementLow)
			}
			measurements = append(measurements, m)
		}
	}
	bytes, err := json.MarshalIndent(measurements, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceQueryContract(t *testing.T) {
	entries := []Entry{
		{ID: "1", Kind: "text", Text: "A\n中文", Title: "Title", Source: "Editor"},
		{ID: "2", Kind: "code", Text: "CODE", Title: "Name", Source: "Terminal"},
		{ID: "3", Kind: "image", Title: "复制的图片", Source: "Editor", Image: []byte{1, 2}},
	}
	got := referenceQuery(entries, "  TITLE editor  ", "all")
	if len(got) != 1 || got[0].ID != "1" || got[0].Text != "" || got[0].Image != nil {
		t.Fatal(got)
	}
	if got := referenceQuery(entries, "", "image"); len(got) != 1 || got[0].ID != "3" {
		t.Fatal(got)
	}
	if got := referenceQuery(entries, "unknown", ""); !reflect.DeepEqual(got, []Entry{}) {
		t.Fatalf("empty shape: %#v", got)
	}
}
