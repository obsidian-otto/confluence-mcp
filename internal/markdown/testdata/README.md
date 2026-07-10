# testdata — golden-file fixtures for the markdown round-trip

Each fixture directory contains:

  - `in.md`      — the Markdown input string
  - `want.xhtml` — expected output of MarkdownToStorageXHTML(in.md)
  - `want.xhtml.md` — expected output of StorageXHTMLToMarkdown(want.xhtml)

The 28-fixture directory layout mirrors the acon reference
implementation's feature-support matrix. Each fixture is one self-
contained CommonMark construct so round-trip fidelity for that
construct can be asserted in isolation.

## Categories (28 total)

| # | dir                       | covers                                |
|---|---------------------------|---------------------------------------|
| 01 | headings                  | # H1 / ## H2 / ### H3 ...             |
| 02 | headings-deep             | H1..H6 in one document                |
| 03 | code-blocks               | ```go ... ``` with language            |
| 04 | code-blocks-no-lang       | ``` ... ``` (no language hint)         |
| 05 | code-blocks-multiline     | long, multi-paragraph code            |
| 06 | tables                    | GFM table (one header row + data)     |
| 07 | tables-headers-and-data   | GFM table multiple rows               |
| 08 | links                     | external markdown link                |
| 09 | links-relative            | relative URL markdown link            |
| 10 | images                    | bare markdown image                   |
| 11 | images-with-alt           | markdown image with alt text          |
| 12 | task-lists                | GFM unchecked task                    |
| 13 | task-lists-checked        | GFM checked task                      |
| 14 | blockquotes               | single-level blockquote               |
| 15 | blockquotes-nested        | nested blockquote                     |
| 16 | horizontal-rules          | --- rule                              |
| 17 | strikethrough             | ~~strike~~                            |
| 18 | inline-code               | `code`                                |
| 19 | inline-code-backticks     | code containing literal backticks     |
| 20 | unicode                   | CJK + accented chars                  |
| 21 | emoji                     | 🎉 literal emoji                      |
| 22 | html-entities              | &amp; &lt; &gt; in code block         |
| 23 | snake-case-identifiers    | the regression test for h2m escaping  |
| 24 | lists-unordered           | - a / - b / - c                       |
| 25 | lists-ordered             | 1. / 2. / 3.                          |
| 26 | lists-nested              | nested unordered+ordered               |
| 27 | emphasis-strong-italic    | **bold**, *italic*, and ***both***    |
| 28 | paragraphs                | multi-paragraph document              |

## How to update goldens

```bash
go test -tags update ./internal/markdown/...
```

The `//go:build update` tag enables `TestUpdateGoldens`, which
rewrites every fixture's `want.xhtml` and `want.xhtml.md` from the
current pipeline output. Use this when the post-processor or h2m
library has changed in a way that intentionally alters the output.
