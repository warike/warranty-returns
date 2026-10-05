package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func answers(match float64, pick string, pickP float64, score float64) map[string]Answer {
	return map[string]Answer{
		"match":  {Kind: KindYesNo, Yes: match},
		"cause":  {Kind: KindChoice, Pick: pick, Probs: map[string]float64{pick: pickP}},
		"refuse": {Kind: KindScore, Score: score},
	}
}

func TestPolicy(t *testing.T) {
	cases := []struct {
		name   string
		a      map[string]Answer
		want   Action
		reason string
	}{
		{"clear factory fault pays", answers(0.95, "factory", 0.90, 0.2), Pay, "factory 0.90"},
		{"at every threshold pays", answers(0.70, "factory", 0.70, 1.0), Pay, "refuse 1.0"},
		{"misuse goes to a person", answers(0.90, "misuse", 0.95, 3.8), Review, "cause misuse"},
		{"shipping goes to a person", answers(0.90, "shipping", 0.80, 2.0), Review, "cause shipping"},
		{"nothing found goes to a person", answers(0.20, "none", 0.85, 3.1), Review, "cause none"},
		{"factory but unsure cause", answers(0.90, "factory", 0.55, 0.5), Review, "factory only 0.55"},
		{"factory but fault does not match", answers(0.40, "factory", 0.85, 0.8), Review, "fault match 0.40"},
		{"factory but refuse score high", answers(0.90, "factory", 0.85, 2.4), Review, "refuse score 2.4"},
		{"missing answer fails closed", map[string]Answer{"match": {Kind: KindYesNo, Yes: 1}}, Review, "incomplete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Policy(tc.a)
			if d.Action != tc.want || !strings.Contains(d.Reason, tc.reason) {
				t.Fatalf("got %+v, want %s containing %q", d, tc.want, tc.reason)
			}
		})
	}
}

func TestLevelName(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0.0, "clearly pay"},
		{1.05, "probably pay"},
		{2.6, "probably refuse"},
		{4.0, "clearly refuse"},
		{4.6, "?"},
	}
	for _, tc := range cases {
		if got := levelName(Answer{Kind: KindScore, Score: tc.score}); got != tc.want {
			t.Errorf("levelName(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

const reply = `{"model":"jev-1.13.0","answers":{
  "match":{"type":"noul","noul":0.97},
  "cause":{"type":"choice","choice":"factory","probabilities":{"factory":0.96,"misuse":0.02,"shipping":0.01,"none":0.01}},
  "refuse":{"type":"score","score":0.1,"probabilities":{"0":0.9,"1":0.1,"2":0,"3":0,"4":0}}
},"usage":{"input_tokens":300,"output_tokens":12}}`

func fakeServer(t *testing.T, body string, got *wireRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			if err := json.NewDecoder(r.Body).Decode(got); err != nil {
				t.Errorf("decode request: %v", err)
			}
		}
		w.Write([]byte(body))
	}))
}

func TestScorer_SendsQuestionsAndMapsAnswers(t *testing.T) {
	var got wireRequest
	srv := fakeServer(t, reply, &got)
	defer srv.Close()
	s := Jev("secret")
	s.url = srv.URL

	a, err := s.Score(context.Background(), returns[0], questions)
	if err != nil {
		t.Fatal(err)
	}
	levels, ok := got.Questions["refuse"].Criteria.([]any)
	if !ok || len(levels) != 5 {
		t.Fatalf("score levels sent = %+v", got.Questions["refuse"].Criteria)
	}
	if a["match"].Yes != 0.97 || a["cause"].Pick != "factory" || a["refuse"].Score != 0.1 {
		t.Fatalf("answers = %+v", a)
	}
	if d := Policy(a); d.Action != Pay {
		t.Fatalf("decision = %+v", d)
	}
}

func TestScorer_UnwrapsCloudflareEnvelope(t *testing.T) {
	srv := fakeServer(t, `{"result":`+reply+`,"success":true,"errors":[]}`, nil)
	defer srv.Close()
	s := Clef("acc", "tok", "clef")
	s.url = srv.URL
	if _, err := s.Score(context.Background(), returns[0], questions); err != nil {
		t.Fatal(err)
	}
}

func TestScorer_RejectsBadAnswers(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"missing answer", `{"answers":{}}`, `no answer for "match"`},
		{"choice outside options", strings.Replace(reply, `"choice":"factory"`, `"choice":"legal"`, 1), `picked "legal"`},
		{"score above top level", strings.Replace(reply, `"score":0.1`, `"score":4.5`, 1), `bad score`},
		{"probability outside unit range", strings.Replace(reply, `"factory":0.96`, `"factory":1.4`, 1), `outside [0,1]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeServer(t, tc.body, nil)
			defer srv.Close()
			s := Jev("secret")
			s.url = srv.URL
			_, err := s.Score(context.Background(), returns[0], questions)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
