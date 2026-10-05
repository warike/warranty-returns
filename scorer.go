package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Kind is the question type, named as on the wire.
type Kind string

const (
	KindYesNo  Kind = "noul"
	KindChoice Kind = "choice"
	KindScore  Kind = "score"
)

// Question is one fixed question. Criteria maps option to description for a
// choice, or holds "true"/"false" examples for yes/no. Levels is the ordered
// scale for a score, lowest first.
type Question struct {
	ID           string
	Kind         Kind
	Instructions string
	Criteria     map[string]string
	Levels       []string
}

// Answer is a probability per allowed answer. Yes for yes/no; Pick and Probs
// for choice; Score (0 to len(Levels)-1) and Probs by level index for score.
type Answer struct {
	Kind  Kind
	Yes   float64
	Pick  string
	Score float64
	Probs map[string]float64
}

// Scorer calls a decision-model API. Jev and Clef share the request and
// response shape; they differ in URL, key, model name and Cloudflare's
// {"result": ...} wrapper.
type Scorer struct {
	Name     string
	url      string
	token    string
	model    string
	envelope bool
	client   *http.Client
}

// Jev returns a scorer for TypeSafe's hosted API.
func Jev(apiKey string) *Scorer {
	return &Scorer{
		Name:   "jev",
		url:    "https://api.typesafe.ai/v1/systemone",
		token:  apiKey,
		model:  "jev-latest",
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Clef returns a scorer for a Cloudflare Workers AI model: "clef" or
// "clef-flash".
func Clef(accountID, token, model string) *Scorer {
	return &Scorer{
		Name:     model,
		url:      fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/@cf/cloudflare/%s", accountID, model),
		token:    token,
		model:    model,
		envelope: true,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

// Wire types, kept apart from Question/Answer so the HTTP shape can change
// without touching the policy.

type wireQuestion struct {
	Type         Kind   `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"` // map for choice/yes-no, ordered list for score
}

type wireRequest struct {
	State     string                  `json:"state"`
	Model     string                  `json:"model"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireAnswer struct {
	Type          Kind               `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        string             `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type wireResponse struct {
	Answers map[string]wireAnswer `json:"answers"`
}

type cloudflareEnvelope struct {
	Result  json.RawMessage `json:"result"`
	Success bool            `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Score sends every question in one request and maps the typed answers back.
// An answer of the wrong kind, or outside its range, is an error.
func (s *Scorer) Score(ctx context.Context, state string, qs []Question) (map[string]Answer, error) {
	req := wireRequest{State: state, Model: s.model, Questions: make(map[string]wireQuestion, len(qs))}
	for _, q := range qs {
		wq := wireQuestion{Type: q.Kind, Instructions: q.Instructions}
		if q.Kind == KindScore {
			if len(q.Levels) < 2 {
				return nil, fmt.Errorf("%s: %q needs at least two levels", s.Name, q.ID)
			}
			wq.Criteria = q.Levels
		} else if len(q.Criteria) > 0 {
			wq.Criteria = q.Criteria
		}
		req.Questions[q.ID] = wq
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.Name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %s", s.Name, resp.StatusCode, firstBytes(raw, 300))
	}
	if s.envelope {
		var env cloudflareEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("%s: envelope: %w", s.Name, err)
		}
		if !env.Success {
			return nil, fmt.Errorf("%s: %v", s.Name, env.Errors)
		}
		raw = env.Result
	}
	var wr wireResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return nil, fmt.Errorf("%s: body: %w", s.Name, err)
	}

	out := make(map[string]Answer, len(qs))
	for _, q := range qs {
		wa, ok := wr.Answers[q.ID]
		if !ok {
			return nil, fmt.Errorf("%s: no answer for %q", s.Name, q.ID)
		}
		if wa.Type != q.Kind {
			return nil, fmt.Errorf("%s: %q answered as %q, asked %q", s.Name, q.ID, wa.Type, q.Kind)
		}
		for k, p := range wa.Probabilities {
			if !unit(p) {
				return nil, fmt.Errorf("%s: %q probability %q=%v outside [0,1]", s.Name, q.ID, k, p)
			}
		}
		switch q.Kind {
		case KindYesNo:
			if wa.Noul == nil {
				return nil, fmt.Errorf("%s: %q missing noul", s.Name, q.ID)
			}
			if !unit(*wa.Noul) {
				return nil, fmt.Errorf("%s: %q bad noul %v", s.Name, q.ID, *wa.Noul)
			}
			out[q.ID] = Answer{Kind: KindYesNo, Yes: *wa.Noul}
		case KindChoice:
			if _, allowed := q.Criteria[wa.Choice]; !allowed {
				return nil, fmt.Errorf("%s: %q picked %q, not an option", s.Name, q.ID, wa.Choice)
			}
			out[q.ID] = Answer{Kind: KindChoice, Pick: wa.Choice, Probs: wa.Probabilities}
		case KindScore:
			top := float64(len(q.Levels) - 1)
			if wa.Score == nil {
				return nil, fmt.Errorf("%s: %q missing score", s.Name, q.ID)
			}
			if *wa.Score < 0 || *wa.Score > top {
				return nil, fmt.Errorf("%s: %q bad score %v, want [0,%v]", s.Name, q.ID, *wa.Score, top)
			}
			out[q.ID] = Answer{Kind: KindScore, Score: *wa.Score, Probs: wa.Probabilities}
		}
	}
	return out, nil
}

func unit(p float64) bool { return p >= 0 && p <= 1 }

func firstBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// scorersFromEnv builds every backend whose keys are set and reports the
// rest on stderr. Keys: TYPESAFE_API_KEY for Jev; CLOUDFLARE_ACCOUNT_ID and
// CLOUDFLARE_API_TOKEN for Clef and Clef-flash.
func scorersFromEnv() []*Scorer {
	var out []*Scorer
	if key := os.Getenv("TYPESAFE_API_KEY"); key != "" {
		out = append(out, Jev(key))
	} else {
		fmt.Fprintln(os.Stderr, "skip jev: TYPESAFE_API_KEY not set")
	}
	account, token := os.Getenv("CLOUDFLARE_ACCOUNT_ID"), os.Getenv("CLOUDFLARE_API_TOKEN")
	if account != "" && token != "" {
		out = append(out, Clef(account, token, "clef"), Clef(account, token, "clef-flash"))
	} else {
		fmt.Fprintln(os.Stderr, "skip clef, clef-flash: CLOUDFLARE_ACCOUNT_ID / CLOUDFLARE_API_TOKEN not set")
	}
	return out
}

// loadDotEnv sets KEY=VALUE lines from ./.env into the environment without
// overriding variables already set. No file is fine.
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || os.Getenv(k) != "" {
			continue
		}
		os.Setenv(k, strings.Trim(strings.TrimSpace(v), `"'`))
	}
}
