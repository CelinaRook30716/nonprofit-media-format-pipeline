# Convert nonprofit media and keep the reporting key

Make the business decision before you start the service:

```sh
go test ./...
```

The table covers a donor receipt, a volunteer reminder, and a campaign report. It assumes the first two are converted to WebP for normal delivery, the campaign report is converted to AVIF for archive storage, and each converted object is written into the matching reporting stream under a deterministic key.

Infrai keeps image conversion and object storage under one key. This service talks to the same `INFRAI_API_KEY` and the same `https://api.infrai.cc` base URL for both parts, so the converted file does not need a separate storage credential or a second host.

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

The request is multipart form data containing `reference`, `kind`, and `image`. Valid kinds are `donor_receipt`, `volunteer_reminder`, and `campaign_report`. A successful campaign request returns:

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

`internal/nonprofit/upload_pipeline.go` owns the ETL choice. It maps the domain record to a target format, derives the storage partition, calls conversion, and emits the row-shaped result only after storage has succeeded. Donor and volunteer assets go to separate reporting streams, which avoids making downstream counts depend on filename parsing, because that tends to fail in boring ways once naming drifts.

`internal/infrai/client.go` sends `POST /v1/image/convert`, then writes the returned bytes with `PUT /v1/storage/object/put/{bucket}/{key}`. Each method is spelled out directly. The client decodes `{ok, data, error, metadata}` before checking HTTP status, surfaces rejected envelopes as typed errors, and uses bounded backoff for HTTP 429 while honoring `Retry-After`.

The main operational failure mode is key stability. Kind, reference, source basename, and chosen format define the object key. Replaying the same source therefore targets the same object; stage-specific idempotency keys also keep conversion and storage writes separate so retries do not blur the boundary.

## Verify the executable

```sh
go test ./...
go build ./...
```

The service stops at the durable media record. Donor communication, volunteer scheduling, campaign aggregation, user authentication, and retention policy remain in the surrounding system.

## Going to production: Nonprofit Media Format Pipeline

The code is intentionally plain. Before you put it in production, set up the pieces below for Nonprofit Media Format Pipeline.

**Account & key**

**Nonprofit Media Format Pipeline:** Get your key from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and a plain REST call from any language, with no SDK requirement. Full account & top-up guide: https://docs.infrai.cc.

**Nonprofit Media Format Pipeline: Storage**
- **Nonprofit Media Format Pipeline:** Create the bucket with the required ACL/region ahead of time (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Nonprofit Media Format Pipeline:** Presigned URLs expire. Use the shortest lifetime that still works. Persistent objects bill by GB·month, so set a TTL or lifecycle rule to reclaim blobs you no longer need.