package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// Each return is the customer's form plus the technician's note, as the
// warranty desk would see it.
var returns = []string{
	`Customer: dishwasher stopped heating after 3 months, dishes come out cold.
Technician: heating element open circuit, solder joint on the element failed. No scale, no damage, no signs of misuse. Replaced under warranty.`,

	`Customer: washing machine makes a loud noise and the drum does not spin.
Technician: drum bearing destroyed. Found two bricks and a soaked rug inside the drum; load well above the rated 8 kg. Housing cracked from the inside.`,

	`Customer: fridge is not cooling, food spoiled.
Technician: compressor and thermostat fine, cabinet reaches 4 C in 40 minutes. Door seal has a straight cut about 15 cm long, looks made with a blade. Unit cools as designed with the seal replaced.`,

	`Customer: oven arrived with the glass door cracked.
Technician: crack starts at the lower corner, consistent with an impact. Outer box crushed on the same corner, delivery photos attached. Oven itself works.`,

	`Customer: laptop screen flickers now and then, mostly in the evening.
Technician: two hours on the bench, could not reproduce. No error logs, no physical damage, cable seated. Returned to customer.`,
}

func main() {
	loadDotEnv()
	scorers := scorersFromEnv()
	if len(scorers) == 0 {
		fmt.Fprintln(os.Stderr, "no backend configured; see README")
		os.Exit(2)
	}

	ctx := context.Background()
	failed := false
	for _, state := range returns {
		fmt.Printf("RETURN  %s\n", state)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  \t\tmatch\tcause\trefuse\taction\treason")
		for _, s := range scorers {
			start := time.Now()
			answers, err := s.Score(ctx, state, questions)
			took := time.Since(start).Round(time.Millisecond)
			if err != nil {
				failed = true
				fmt.Fprintf(w, "  %s\t%v\t\t\t\t\terror: %v\n", s.Name, took, err)
				continue
			}
			m, c, r := answers["match"], answers["cause"], answers["refuse"]
			d := Policy(answers)
			fmt.Fprintf(w, "  %s\t%v\tyes %.2f\t%s %.2f\t%.1f %s\t%s\t%s\n",
				s.Name, took, m.Yes, c.Pick, c.Probs[c.Pick], r.Score, levelName(r), d.Action, d.Reason)
		}
		w.Flush()
		fmt.Println()
	}
	if failed {
		os.Exit(1)
	}
}

// levelName is the short name of the level nearest the score, the part
// before the colon.
func levelName(a Answer) string {
	var levels []string
	for _, q := range questions {
		if q.ID == "refuse" {
			levels = q.Levels
		}
	}
	i := int(a.Score + 0.5)
	if i < 0 || i >= len(levels) {
		return "?"
	}
	name, _, _ := strings.Cut(levels[i], ":")
	return name
}
