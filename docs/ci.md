# Publishing from a pipeline

Every build publishes its own preview, and reviewers open a link instead of
downloading a CI artifact and opening it by hand.

## The key

Create it in **Connect agents → Your keys → New key with custom permissions**,
of type **CI pipeline**, and put it in the repository's secrets together with
the instance's address.

A key can do less than the person who issued it: it publishes versions and
reads comments, but cannot delete artifacts, invite people or issue other keys.
That is what makes it bearable to put it somewhere the whole team can see. You
can remove further permissions when creating it, and give it an expiry.

## GitHub Actions

```yaml
name: Preview

on: pull_request

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm ci && npm run build

      - name: Publish the preview to Falconia
        env:
          FALCONIA_API_URL: ${{ secrets.FALCONIA_API_URL }}
          FALCONIA_TOKEN:   ${{ secrets.FALCONIA_TOKEN }}
        run: |
          curl -fsSL "$FALCONIA_API_URL/api/install" | sh
          URL=$(~/.local/bin/falconia deploy ./dist \
                  --space team \
                  --slug "preview-pr-${{ github.event.number }}" \
                  --visibility org \
                  --message "${{ github.event.pull_request.title }}" \
                  --open)
          echo "PREVIEW=$URL" >> "$GITHUB_ENV"

      - name: Comment on the pull request
        uses: actions/github-script@v7
        with:
          script: |
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `Preview: ${process.env.PREVIEW}`,
            })
```

Tying the slug to the pull request number means every push updates the same
artifact instead of creating a new one: whoever already opened the link sees the
latest version without being sent another.

`--open` makes the CLI print only the URL (the share link when general access is
"anyone with the link"), which is what you want when the output goes into a
variable.

## GitLab CI

```yaml
preview:
  image: alpine:3.20
  variables:
    FALCONIA_API_URL: $FALCONIA_API_URL
    FALCONIA_TOKEN: $FALCONIA_TOKEN
  script:
    - apk add --no-cache curl
    - curl -fsSL "$FALCONIA_API_URL/api/install" | sh
    - ~/.local/bin/falconia deploy ./public
        --space team --slug "mr-$CI_MERGE_REQUEST_IID"
        --visibility org --open
```

## Reading comments from a script

```bash
falconia comments team/preview-pr-42
```

The output is text meant to be pasted into an agent's prompt as is: for each
open thread there are the file, the element's selector, the quoted text and the
messages.

## Variables

| | |
| --- | --- |
| `FALCONIA_API_URL` | the instance to talk to |
| `FALCONIA_TOKEN` | the key |
| `FALCONIA_AGENT` | label what this pipeline publishes (e.g. `codex` when Codex runs in CI) |
| `FALCONIA_CONFIG_DIR` | where to keep the configuration, if `~/.falconia` does not suit |

The CLI recognizes that it runs inside a pipeline from the variables every CI
exports, and records deploys as such in the instance's activity log. The legacy
`SFA_*` names still work.
