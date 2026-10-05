# warranty-returns

A warranty desk that pays itself, within limits. A decision model reads the
customer's return form and the technician's note, answers three fixed
questions with probabilities, and plain Go code decides: pay, or hand it to a
person. Code never refuses a customer on its own.

Built to compare two decision models that launched two weeks apart,
[TypeSafe Jev](https://docs.typesafe.ai/models.md) and
[Cloudflare Clef](https://blog.cloudflare.com/clef-decision-models/) (27B and
the 9B Clef-flash), on the same input, the same questions and the same
policy. The write-up with answers, latency and cost is at
[warike.tech/blog/jev-vs-clef-decision-models](https://warike.tech/blog/jev-vs-clef-decision-models).

## The three questions

One state, three typed questions. The model never writes the decision; it
returns a number for each question and the policy reads the numbers.

| Question | Kind | Answer |
|---|---|---|
| Does the fault found match the fault reported? | yes/no | probability of yes |
| What caused it? | choice | `factory`, `misuse`, `shipping`, `none`, one probability each |
| How strongly should this NOT be paid? | score | 0 (clearly pay) to 4 (clearly refuse) |

## The policy

Three thresholds, checked in order. Any miss sends the return to review.

```go
const (
    payMatchAt  = 0.70 // fault reported and fault found agree
    payCauseAt  = 0.70 // "factory" chosen with this much probability
    payRefuseAt = 1.0  // score at or under "probably pay"
)
```

A missing or wrongly typed answer also goes to review. The policy fails
closed: a bad model response costs a read, never a payment.

## Layout

| File | What |
|---|---|
| `scorer.go` | HTTP client. Jev and Clef share one request and one response shape; only the URL, the auth header and Cloudflare's response envelope differ. |
| `policy.go` | The three questions, the thresholds, the decision. |
| `main.go` | Five sample returns, one table per return, one row per model. |
| `policy_test.go` | Policy table tests and a fake HTTP server for the client. No network. |

Standard library only.

## Setup

```sh
cp .env.example .env
```

| Variable | Where it comes from |
|---|---|
| `TYPESAFE_API_KEY` | [TypeSafe dashboard](https://typesafe.ai) |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare dashboard, account home |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token with `Account · Workers AI · Edit` |

A model whose keys are empty is skipped, so one provider is enough to run.
`.env` is gitignored.

## Run

```sh
go test -race ./...   # policy tests, no network
go run .              # five returns, up to three models, one table each
```

Exit code 0 when every configured model answered, 1 when one failed, 2 when
none is configured.

## License

[MIT](LICENSE), Warike UG (haftungsbeschränkt).
