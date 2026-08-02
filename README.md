# anilist

[![Go Reference](https://pkg.go.dev/badge/github.com/icco/anilist.svg)](https://pkg.go.dev/github.com/icco/anilist)
[![Test Go](https://github.com/icco/anilist/actions/workflows/test.yml/badge.svg)](https://github.com/icco/anilist/actions/workflows/test.yml)

A minimal Go client for the [AniList](https://anilist.co) GraphQL API: reads a user's public anime list and their scores, normalized to a 0–10 scale.

No API key, no OAuth, no registration. AniList serves public lists to anonymous callers.

```
go get github.com/icco/anilist
```

## Usage

```go
c := anilist.NewClient()

entries, err := c.List(ctx, "nat")
if err != nil {
  return err
}
for _, e := range entries {
  fmt.Printf("%s (%d): %.1f/10\n", e.Title, e.Year, e.Score)
}
```

## Notes

- **Scores are normalized to 0–10.** Each AniList user picks their own score format — `POINT_100`, `POINT_10`, `POINT_10_DECIMAL`, `POINT_5`, or `POINT_3` — so the same raw number means different things on different accounts. `List` reads the account's `scoreFormat` in the same query and rescales, which is the whole reason this package exists rather than a raw GraphQL call. An unrecognized format is passed through unchanged.
- **Only rated entries come back.** Entries with a score of 0 (AniList's "unrated") are dropped, as are entries with no title in either language.
- **Titles prefer English, falling back to romaji.** Many titles have only one.
- **The user's lists are flattened.** AniList splits a library into named lists (Watching, Completed, Dropped, ...); `List` returns entries from all of them, without saying which list an entry came from.
- **An unknown username is not an error.** AniList answers with HTTP 200 and a null user, so you get an empty slice.
- **Anime only, read only.** No manga, no mutations, no auth. `MediaListCollection` is queried with `type: ANIME`. PRs welcome.
- **`URL` is exported** so you can point the client at a test server.
- No third-party dependencies.

## License

MIT
