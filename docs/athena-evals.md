# Athena's voice and small eval loop

Athena should sound like a sharp, useful assistant, not a release-note generator. This iteration changes the journal's wording policy and adds eight synthetic eval cases. It does not add chat, email, personal-memory storage, new tools, or scheduling.

## Voice agreed with the user

- **Dry, warm, and concise.** A dry nudge rather than a theatrical roast: “Five services. Ambitious. A script would do—here's why.”
- **Make complicated things simple.** Give the shortest useful explanation, generally two or three sentences at most. Use an example or diagram only when it helps. The journal's existing short headline and usual one or two detail sentences still apply.
- **A little personality everywhere.** Occasional dry phrasing belongs in the journal too, but the outcome must stay clear and searchable. No joke quota; plain language is better than forced wit.
- **Banter is not automatically switched off by frustration.** Never be cruel, ridicule the person, or minimize a real consequence. An explicit request to stop should be respected in a future conversational interface.
- **Challenge once, then respect the user's call.** This is a future conversation preference, not permission for the curator to argue with reports, invent advice, or create obligations.

The implemented voice lives in `internal/athena/policy.txt`. The JSON contract, protected entries, human todo authority, and tool-free runner stay unchanged. Reports and preference reasons remain data, not a channel for overriding policy. There is no new runtime sarcasm toggle or conversation interface in this patch.

The policy version is `athena-v2.5`. Existing recovery rules stop unfinished plans from an older policy and leave them explicitly retryable; this patch does not replay them or rewrite journal history.

## Run the cheap checks

From the repository root:

```sh
go test ./internal/athena -run 'TestPersonality(EvalFixtures|JudgeRejectsInvalidVerdicts)$' -count=1 -v
```

This checks the cases, illustrative headlines, policy version, mechanical grading checks, and rejection of malformed/incomplete judge verdicts or invented quotes. It uses temporary stores, makes no model calls, and does **not** demonstrate the model's quality.

## Run the model eval

Use the existing opt-in smoke-test setup:

```sh
DAYLOG_LUNA_SMOKE=1 \
DAYLOG_SMOKE_PI="$HOME/.local/bin/pi" \
DAYLOG_SMOKE_PI_AGENT_DIR="$HOME/.pi/agent" \
go test ./internal/athena -run '^TestOptInAthenaPersonality$' -count=1 -v -timeout 40m
```

Set the two paths to your installed pi and its agent directory. The test uses the repository's default runner provider/model, not settings from your live daylog store. Pi supplies its existing authentication. No fallback provider is added.

The eight cases are in `testdata/gatekeeper/personality.json`. They cover a dry-nudge opportunity, simplifying a technical explanation, a failed fix, routine noise, a duplicate, an existing editorial preference, injected instructions, and a genuinely unclear result. Every run sends synthetic inputs to the model, uses its normal quota/billing, and writes only temporary test data. It never curates your queue or publishes into your journal. A full successful run uses 17 Pi invocations: eight generation calls, eight separate judge calls, and one judge sanity check. Both generation and judging use the same default provider/model, in separate sessions. Pi may retry within its normal timeout, so 17 invocations is not necessarily 17 provider requests or a dollar budget. These direct test calls do not go through the production worker's daily call budget. The suite timeout accommodates the existing per-invocation deadlines; it is not a latency promise.

For a single-case iteration, select its subtest:

```sh
DAYLOG_LUNA_SMOKE=1 \
DAYLOG_SMOKE_PI="$HOME/.local/bin/pi" \
DAYLOG_SMOKE_PI_AGENT_DIR="$HOME/.pi/agent" \
go test ./internal/athena -run '^TestOptInAthenaPersonality$/^dry-nudge$' -count=1 -v
```

A selected single case uses two invocations and does not run the separate sanity-check subtest; run the full suite before accepting changes.

Generation receives the ordinary production policy and synthetic input, not the fixture file's example answers or review fields. However, the policy itself contains near-identical scenarios and phrasing for **dry-nudge** and **make-complexity-simple**. Those cases are labeled **prompt-example regressions**, not independent evidence of generalization. Voice and simplification generalization have not been tested.

The judge receives only the original synthetic input, actual Athena output, and a short judge rubric. It does not inherit the generation conversation, full generation prompt, fixture examples, or human review notes. Its input/output texts remain untrusted data. The test prints the report, expected disposition, review criteria, actual decision, and judge verdicts with reasons and exact supporting quotes.

## Judge it without a framework

Automatic checks cover:

- The same strict output validation used by the worker, including supplied identities, legal references, and allowed actions.
- One expected publish, skip, or hold decision per case.
- Retention of supplied references on published outcomes.

After deterministic checks, a separate tool-free Pi print call judges four criteria:

| Criterion | Pass when |
| --- | --- |
| Fidelity | The output follows from supplied reports, limitations survive, and failed/proposed work does not become completed work. |
| Disposition | The publish/amend/merge/skip/hold choice fits the report, outcomes, protections, and editorial rules. |
| Simplicity | The user-visible consequence is easy to understand; jargon and unnecessary explanation do not hide it. |
| Voice | The wording is concise and respectful, with room for understated dry wit. No insult, pep talk, or joke replacing the facts. |

Each criterion returns a boolean, concise reason, and exact quote from the supplied input/output. A false verdict fails the test. Missing verdicts, malformed JSON, fabricated quotes, process failures, and timeouts fail too; none are converted to passes. Quotes may cite exact content or serialized JSON fields, such as the skip action kind. Judge errors are reported separately from negative quality verdicts, and rejected responses have a bounded diagnostic excerpt because these cases contain synthetic data only. On skip/hold, the judge marks simplicity and voice as passing but not applicable, since there is no journal text; do not count these as measured voice/simplicity successes.

The full suite first gives the judge a deliberately bad, structurally valid output: a still-failing save-loss bug falsely becomes a deployed success, with an insult added. The judge must reject both fidelity and voice. If it does not, the suite stops before spending calls on the eight generated outputs. This catches an always-pass judge, not every grading failure.

The judge uses a fresh temporary working directory and the existing runner's no-tools, no-skills, no-extensions, no-context-files, and empty append-system-prompt flags. Its custom rubric replaces the generation system prompt for that invocation only. No shared runner, production schema, dependencies, services, registry, dashboard, or scoring engine are added.

A green run means the deterministic checks and **fallible LLM assessments** passed, not that the facts were independently verified. Generation and judge use the same model and can share biases. Exact-quote checks establish that cited text exists, not that the verdict follows logically. Human inspection of the printed reasons remains important. A plain entry may pass; a completely bland set can still need voice work. Do not require particular joke words or exact example headlines.

## Patterns checked against Lovable

Read-only inspection of the existing Lovable Go eval implementation informed these small choices (paths below are in that repository, not dependencies):

- `go/api/pkg/eval/evaluators/responsejudge/evaluator.go`: explicit binary rubric, original context alongside the actual response, and untrusted-input boundaries.
- `go/api/pkg/eval/evaluators/llmjudge/prompts.go`: separate judge instructions from evaluated content; require every configured criterion and concise reasons.
- `go/api/pkg/eval/evaluator/judge_template.go` and `errored_outcome_test.go`: invocation/parsing errors must stay errors, not become successful scores or disappear.
- `go/api/pkg/eval/evaluators/screenshotjudge/calibration_test.go`: exercise the real rubric against fixed controls. Athena uses a tiny synthetic bad-output control rather than screenshots or remote artifacts.
- `go/api/pkg/integrationmock/llmjudge/llmjudge.go` and `llmjudge_test.go`: a bounded one-shot judge with typed verdict parsing and parser tests. Athena keeps strict JSON rather than extracting a JSON-looking substring from surrounding prose.

Deliberately not borrowed: registries, evidence-projection frameworks, score aggregation, hosted result storage, browser/sandbox judges, provider selection/fallback, structured submission tools, or retry/repair loops. No Lovable source or fixtures are copied into model requests, no Lovable evals or services are run, and no cross-repository imports are added. Not-applicable criteria are not evidence of quality; this suite intentionally has no aggregate percentage to inflate.

A minimal review note is enough:

```text
Policy/model: ...
Case                         Auto   Fidelity   Disposition   Simplicity   Voice
make-complexity-simple        ...    ...        ...           ...          ...
What missed, and the next prompt/example change: ...
```

Run a case, inspect the actual output and judge's quoted reasons, change one thing, rerun it, then run the whole set. Do not soften grading or add retries just to get a green run; report disagreements and instability. Existing `TestOptInLunaSmoke` and `TestOptInEditorialConsolidation` remain useful regression checks for broader editorial behavior. Repeat important cases when needed: one passing sample does not establish consistency, and eight cases are not a security guarantee.

The preference case supplies an existing editorial example. It does not test durable personal memory, preference learning, a no-banter command, conversational explanations, or follow-through. Those need their own scoped implementation and cases if added later.
