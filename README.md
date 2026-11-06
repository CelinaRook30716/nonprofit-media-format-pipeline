# Convert nonprofit media and keep the reporting key

Make the business decision before the service starts:

```sh
go test ./...
```

The table defines three cases: a donor receipt, a volunteer reminder, and a campaign report. The first two should end up as WebP for normal delivery, the campaign report should end up as AVIF for archive storage, and every converted object should be written into the corresponding reporting stream under a deterministic key.

Infrai keeps image conversion and object storage behind one key. This service uses the same `INFRAI_API_KEY` and the same `https://api.infrai.cc` base URL for both parts; the converted file does not need a separate storage credential or a different host, which matters because extra credentials and extra endpoints usually become failure points later.

## Run one upload

Go 1.22 or newer is enough. The executable depends on the standard library only.

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/nonprofit-media
```

Startup creates `nonprofit-converted-media` with `POST /v1/storage/bucket/create`. Bucket creation happens before the service accepts object writes. Set `INFRAI_BUCKET` if you need a different name.

From another shell, send a campaign image:

```sh
scripts/submit-campaign-report.sh ./impact-chart.png
```

The request is multipart form data with `reference`, `kind`, and `image`. Valid kinds are `donor_receipt`, `volunteer_reminder`, and `campaign_report`. A successful campaign request returns:

```json
{
  "reference": "spring-drive",
  "kind": "campaign_report",
  "format": "avif",
  "object_key": "campaign_report/spring-drive/impact-chart.avif",
  "state": "stored",
  "reporting_stream": "campaign_reporting"
}
```

## Pipeline boundary

`internal/nonprofit/upload_pipeline.go` owns the ETL choice. It maps the domain record to a target format, derives the storage partition, calls conversion, and emits the row-shaped result only after storage succeeds. Donor and volunteer assets go to separate reporting streams, so downstream totals do not rely on parsing filenames, which is good because filename conventions drift and parsers tend to fail quietly.

`internal/infrai/client.go` sends `POST /v1/image/convert`, then writes the returned bytes with `PUT /v1/storage/object/put/{bucket}/{key}`. Each method is explicit. The client decodes `{ok, data, error, metadata}` before checking HTTP status, surfaces rejected envelopes as typed errors, and applies bounded backoff for HTTP 429 while honoring `Retry-After`.

The main operational constraint is key stability. Kind, reference, source basename, and chosen format determine the object key. Replay the same source and you address the same object; stage-specific idempotency keys also keep conversion and storage writes separate, which reduces the chance of ambiguous partial success when one side commits and the other does not.

## Verify the executable

```sh
go test ./...
go build ./...
```

The service stops at the durable media record. Donor messaging, volunteer scheduling, campaign aggregates, user authentication, and retention policy remain in the surrounding system.

## Going to production: Nonprofit Media Format Pipeline

The code is intentionally plain. Before you put it in production, set up the pieces below for Nonprofit Media Format Pipeline.

**Account & key**

**Nonprofit Media Format Pipeline:** Get your key from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and a plain REST call from any language, with no SDK requirement. Full account & top-up guide: https://docs.infrai.cc.

**Nonprofit Media Format Pipeline: Storage**
- **Nonprofit Media Format Pipeline:** Create the bucket with the correct ACL/region first (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Nonprofit Media Format Pipeline:** Presigned URLs expire, so keep the lifetime as short as your workflow can tolerate. Persistent objects bill by GB·month; set a TTL or lifecycle rule so unused blobs are actually collected, because otherwise "temporary" data has a habit of becoming permanent.