# Content removal requests

YAVCHN accepts requests concerning its copies of upstream articles, comments and attribution at the contact address in the privacy policy. A request does not change the upstream site. Operators keep correspondence privately, outside the repository, and collect only the verification needed for the action. Reversible public suppression does not require collecting an identity document. Exporting personal data or deleting an account requires the account's existing authentication controls.

## Operator actions

Publisher article and host exclusions use the existing `takedown add` command. Contributor and item exclusions currently support Hacker News. Other sources must implement the same gates before the command accepts them. A username is scoped to its source and matched exactly; it is not evidence that similarly named accounts elsewhere belong to the same person.

```powershell
docker exec yavchn /yavchn takedown author add hn example-user
docker exec yavchn /yavchn takedown item add hn 12345
docker exec yavchn /yavchn takedown author list
docker exec yavchn /yavchn takedown item list
docker exec yavchn /yavchn takedown author audit hn example-user
docker restart yavchn
```

An author exclusion suppresses that source account's submissions, comments, attribution, profile and replies-watcher output. An item exclusion suppresses one item regardless of its current author metadata. Neither operation changes the upstream content or deletes another reader's notes. The database keeps only the source, kind, identifier and creation date. `remove` in place of `add`, followed by a restart, reverses an exclusion.

Exclusions load before background fetching at startup. Restart after changing them; this also discards the process's upstream item, thread and profile caches. Excluded items are rejected before caching and excluded comments are omitted from fetched discussions. Replies by other authors survive in order. The generic article extractor refuses HN source pages and redirects to them, so it cannot bypass the source adapter's exclusions by copying a whole discussion as an article.

## Completing a request

1. Record the request privately with its received date, scope, action and verification outcome. Keep correspondence and verification evidence out of public Git history.
2. Check the listed source identifiers and choose an author exclusion, item exclusions or publisher exclusions. Keep the current suppression register separately from database backups so a restore cannot erase later requests.
3. Apply the exclusions independently to beta and production, restart both, and verify every public hostname, the direct pages, window fragments, discussion endpoints, profiles and search. Confirm that unrelated replies remain available.
4. Inspect cached article copies for extracted source pages and the requested identifiers. Do not treat hiding a public page as proof that every stored copy has been erased. Review private saved metadata and quotations separately; do not silently delete other people's notes.
5. Where content was indexed, request removal or refresh through the site's search-engine ownership tools. Do not block crawler access with robots.txt before it can see a removal. A removed direct resource returns 410, `X-Robots-Tag: noindex` and `Cache-Control: no-store`. Search engines control their own refresh timing.
6. Confirm only verified outcomes to the requester. State any remaining scope, such as third-party search caches, upstream originals, quotations by others, backups or previously saved browser copies.

## Restore and retention

Before exposing a restored database, reapply every exclusion from the current private suppression register and restart the service. Keep old backups offline. Apply the published backup-retention policy through the operator's scheduled task; the existence of `prune-backups.ps1` alone does not prove that task runs. Removing live content does not authorize permanent deletion of backups or unrelated account data.

## Remaining policy decisions

YAVCHN should obtain a review of its grounds for republishing full articles and comments. Attribution and an accessible API do not by themselves establish a reproduction licence. The review should also decide whether third-party mirror pages should be excluded from search indexing by default, how quotations and identifying text posted by somebody else should be handled, and which non-Singapore laws apply to this service. This implementation does not decide those legal questions.

References include [Singapore's explanation of deletion requests under the PDPA](https://www.mddi.gov.sg/newsroom/pq-on-provision-for-deletion-of-personal-data-upon-request/), [IPOS guidance on using third-party copyright works](https://www.ipos.gov.sg/about-ip/copyright/ownership-and-commercialisation/) and [Google's removal guidance](https://developers.google.com/search/docs/crawling-indexing/remove-information).
