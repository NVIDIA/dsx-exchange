# DSX Exchange Writing Guide

DSX Exchange uses the plain-language principles in
[ASD-STE100 Issue 9](https://www.asd-ste100.org/assets/files/ASD-STE100_ISSUE9.pdf)
for software engineering.
DSX Exchange does not claim full ASD-STE100 compliance.

Use repository terms, software identifiers, API names, and necessary domain
terms as technical nouns or technical verbs.
Do not copy the ASD-STE100 dictionary or its examples into this repository.
This guide is the DSX Exchange source of truth for changed prose.

## Scope and Review Policy

Apply this guide when you add or modify the following text:

- Code comments.
- Test titles.
- Pull request descriptions and comments.
- Changelog entries and announcements.
- Contributor guidance, agent guidance, and user documentation.

Do not request unrelated language cleanup in a feature, fix, documentation, or
release pull request.
Put existing language debt in a focused follow-up pull request.

Language findings are suggestions unless ambiguity can change behavior,
security, data safety, test meaning, or release meaning.
A blocking comment must name that effect.
A suggestion should include a proposed rewrite.

### Full-Corpus Audits

Use full-corpus audit mode only when the task explicitly requests an audit of
existing text.
The changed-text scope does not apply to the assigned author-maintained sources
during that audit.

- Review unchanged text in the assigned corpus.
- Preserve literal identifiers, commands, output, API fields, quotations, and
  official third-party names.
- Exclude generated files unless the task assigns their source or generator.
- Preserve accurate historical statements. Report them only when their meaning
  is incorrect or can misdirect a current action.
- Group repeated low-impact language debt into one finding with representative
  evidence.
- Report findings without editing the audited text unless the task also
  authorizes edits.

## Writing Rules

1. Use one term for one concept. Do not use synonyms to add variety.
2. Use a term with one meaning in a given context.
3. Use the shortest familiar term that preserves the technical meaning.
4. Name the actor when known. Use passive voice only when the actor is unknown
   or does not matter.
5. Put one instruction in each sentence. Split actions that occur at different
   times.
6. Keep instructions at 20 words or fewer when possible. Keep descriptions at
   25 words or fewer when possible.
7. State a condition before the action that depends on it.
8. Use `must` for a requirement, `may` for permission, `can` for capability,
   and `should` for a recommendation.
9. Name the object of relative terms such as `current`, `latest`, `previous`,
   and `next`.
10. Replace `ready`, `clean`, `safe`, `small`, and similar judgments with the
    condition that makes them true.
11. Remove `just`, `simply`, `obviously`, `clearly`, `easy`, `robust`, and
    other words that do not change the meaning.
12. Avoid an idiom or phrasal verb that can have more than one meaning. Use a
    direct technical term when one is available.
13. Use a vertical list for three or more conditions, actions, or results.
14. In a code comment, explain a constraint, invariant, or reason that the
    code does not show. Do not restate the code.

Sentence lengths are review targets, not mechanical limits.
Do not make a sentence less accurate to meet a word count.
Quoted user text, external text, code, identifiers, commands, URLs, and
generated content are outside the word and sentence rules.

## Product Terms

Use the following names consistently:

- **DSX Exchange** is the product and documentation umbrella.
- **DSX Event Bus** is the NATS-based messaging product.
- **DSX Agent Gateway** is the gateway product for agent traffic.
- **AsyncAPI schema** describes a message contract under `schemas/asyncapi/`.
- **Common Services Cluster (CSC)** is the shared-services deployment layer.
- **Control Plane Cluster (CPC)** is a site-specific control-plane deployment
  layer.

Spell out CSC and CPC on first use on each page unless the audience and context
make the abbreviation unambiguous.
Preserve upstream names such as NATS, MQTT, Kubernetes Gateway API, Helm, Kind,
and Skaffold.

## Rewrite Examples

These examples use recurring DSX Exchange concepts.
They show the required level of precision.

| Surface | Avoid | Use |
|---|---|---|
| Code comment | `// Handle the edge case.` | `// Keep the listener disabled when the Gateway has no assigned address.` |
| Code comment | `// This is needed for safety.` | `// Reject overlapping exports because they create cyclic NATS imports.` |
| Code comment | `// Keep this in sync.` | `// This port must match the MQTT listener in the event-bus chart.` |
| Code comment | `// Use the latest state.` | `// Read the Gateway status again before selecting its address.` |
| Test title | `handles invalid config correctly` | `rejects an event-bus account with no public key` |
| Test title | `works after retry` | `reconnects the CPC leaf node after the CSC listener restarts` |
| Test title | `covers edge cases` | `rejects overlapping cross-layer subject patterns` |
| Test title | `does the right thing for auth` | `denies an MQTT publish outside the account subject allowlist` |
| Pull request discussion | `This seems brittle.` | `This lookup assumes every Gateway has an assigned address. Pending Gateways cause an empty endpoint.` |
| Pull request discussion | `Can we clean this up?` | `These templates build the same listener name. Define the name in one Helm helper.` |
| Pull request discussion | `Make this more robust.` | `Return an error when the bridge cannot resolve the upstream service, and add a resolution-failure test.` |
| Pull request discussion | `This is a small change.` | `This change updates one Helm value and does not change the rendered Service ports.` |
| Announcement | `Improved local deployment.` | `Local deployment now reuses unchanged Agent Gateway images.` |
| Release entry | `Fixed various issues.` | `The Agent Gateway chart now rejects a listener with no protocol.` |
| Release entry | `Better error handling.` | `The bridge now reports the upstream connection error before it exits.` |
| Procedure | `Refresh and rerun as needed.` | `Wait for all three site releases to become ready. Run the functional tests again.` |
