# TSIngest E2E Test Report

Date: 2026-08-05 (Asia/Shanghai)

## Environment

- Application image: `tsingest:healthcheck`
- Source: `sample_clip/two_audio_stream.ts`
- Transport: SRT caller to SRT listener
- Listener latency: 200 ms
- No-data timeout: 10 seconds
- MP4 generation: manual

## Verified behavior

1. A recording remained in `waiting_input` before a sender connected.
2. After real TS bytes and media time advanced across consecutive FFmpeg progress samples, the recording entered `recording`.
3. During recording, `progressMs`, `progressSize`, `progressBitrate`, and `lastProgressAt` advanced.
4. Sender termination produced `source_disconnect`; the recording entered finalization and completed as `ready`.
5. The `.part.ts` file was validated and atomically completed as `.ts`.
6. Manual MP4 generation completed as `ready` without modifying the TS master.

## Media verification

| Output | Duration | Size | Video | Audio |
|---|---:|---:|---|---|
| TS | 11.920 s | 13,392,932 bytes | H.264 High, 1920×1080 | 2 × AAC stereo, 48 kHz |
| MP4 | 11.920 s | 13,020,015 bytes | H.264 High, 1920×1080 | 2 × AAC stereo, 48 kHz |

Additional interruption run: a live sender was suspended after the recording reached `recording`; the input terminated, the TS finalized as `ready`, and the end reason was `source_disconnect`.

## Automated verification

- Go tests: passed, including media-progress qualification and recording-exit classification tests.
- React/TypeScript production build: passed.
- Docker image capability checks: passed for SRT, MPEG-TS, MP4, H.264 and AAC.
