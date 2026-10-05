// Command warranty_returns checks a warranty return before money moves. A
// decision model answers three fixed questions about the return form and the
// technician's note; code in this file decides if the refund is paid without
// a human. Code never refuses a customer on its own: a doubtful return goes
// to a person.
package main

import "fmt"

// Questions are fixed. The same three are asked of every return.
var questions = []Question{
	{
		ID: "match", Kind: KindYesNo,
		Instructions: "Does the fault the technician found match the fault the customer reported?",
		Criteria: map[string]string{
			"true":  "customer says it stopped heating, technician finds a dead heating element",
			"false": "customer says it stopped heating, technician finds it heats fine but the door is broken",
		},
	},
	{
		ID: "cause", Kind: KindChoice,
		Instructions: "What caused the fault, according to the technician's note?",
		Criteria: map[string]string{
			"factory":  "a part failed on its own, no outside damage",
			"misuse":   "used outside its limits, forced, overloaded, modified",
			"shipping": "damaged in transport, crushed box, impact marks",
			"none":     "technician found nothing wrong",
		},
	},
	{
		ID: "refuse", Kind: KindScore,
		Instructions: "How strongly does the evidence say this return should NOT be paid under warranty?",
		Levels: []string{
			"clearly pay: factory fault, nothing suspicious",
			"probably pay: small doubts",
			"unclear: evidence cuts both ways",
			"probably refuse: misuse or no fault likely",
			"clearly refuse: misuse or damage proven",
		},
	},
}

// Action is what the code lets happen: Pay moves money with no human,
// Review hands the file to one.
type Action string

const (
	Pay    Action = "pay"
	Review Action = "review"
)

// Decision is an action plus the reason the thresholds produced it.
type Decision struct {
	Action Action
	Reason string
}

// Thresholds are product decisions. Every one must clear for money to move.
const (
	payMatchAt  = 0.70 // fault reported and fault found agree
	payCauseAt  = 0.70 // "factory" chosen with this much probability
	payRefuseAt = 1.0  // score at or under "probably pay"
)

// Policy decides one return from its three answers.
func Policy(a map[string]Answer) Decision {
	match, cause, refuse := a["match"], a["cause"], a["refuse"]
	if match.Kind != KindYesNo || cause.Kind != KindChoice || refuse.Kind != KindScore {
		// Fail closed: a missing or odd answer costs a read, never a payment.
		return Decision{Review, "incomplete answers, a human looks"}
	}
	causeP := cause.Probs[cause.Pick]
	switch {
	case cause.Pick != "factory":
		return Decision{Review, fmt.Sprintf("cause %s %.2f, refuse score %.1f", cause.Pick, causeP, refuse.Score)}
	case causeP < payCauseAt:
		return Decision{Review, fmt.Sprintf("factory only %.2f, under %.2f", causeP, payCauseAt)}
	case match.Yes < payMatchAt:
		return Decision{Review, fmt.Sprintf("fault match %.2f, under %.2f", match.Yes, payMatchAt)}
	case refuse.Score > payRefuseAt:
		return Decision{Review, fmt.Sprintf("refuse score %.1f, over %.1f", refuse.Score, payRefuseAt)}
	}
	return Decision{Pay, fmt.Sprintf("factory %.2f, match %.2f, refuse %.1f", causeP, match.Yes, refuse.Score)}
}
