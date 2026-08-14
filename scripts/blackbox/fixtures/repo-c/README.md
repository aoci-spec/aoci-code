# scale-shop

Frozen large fixture for the AOCI black-box lifecycle harness. It exists so the
multi-batch authoring path and the relation-closure replan path can be exercised
at the real machine batch limit, which the small fixtures cannot reach.

The service is a layered TypeScript order platform: every business domain under
`src/domains/<domain>/` carries the same seven layers (model, repository,
service, handler, validator, mapper, policy), and the layering is the natural
relation shape the harness uses when it authors `R:` fields.

Generated once by `scripts/blackbox/generate_repo_c.py` and frozen. Regenerating
changes the fixture identity and the scale scenario expectations with it.
