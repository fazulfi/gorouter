package media

import (
	"context"
	"io"
)

// BodyCeiling is the transport-safe request/response body ceiling for media
// traffic: approximately 128 MB, the upstream platform request-body limit
// (audit/00-upstream-audit.md:67, next.config.mjs "Request-body limit
// configurable by environment, defaulting to approximately 128 MB"). No
// per-modality upstream numeric limit exists in the tree, so this platform
// authority is the ceiling every media transfer is bounded by
// (limit_defined: false at modality level; phase-3-design.md §10.4).
const BodyCeiling int64 = 128 << 20

// BodyCeilingAuthority is the exact citation for BodyCeiling.
const BodyCeilingAuthority = "audit/00-upstream-audit.md:67 (next.config.mjs request-body limit ~128 MB)"

const copyChunkSize = 32 * 1024

// CopyLimited streams src to dst in bounded chunks, aborting with a typed
// 413-class error as soon as max bytes would be exceeded, without buffering
// the full payload. Context cancellation aborts the copy with ctx.Err().
func CopyLimited(ctx context.Context, dst io.Writer, src io.Reader, max int64, authority string) (int64, error) {
	buf := make([]byte, copyChunkSize)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if written+int64(n) > max {
				return written, NewPayloadTooLargeError(max, authority)
			}
			wn, werr := dst.Write(buf[:n])
			written += int64(wn)
			if werr != nil {
				return written, werr
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return written, nil
			}
			return written, rerr
		}
	}
}

// ReadLimited reads at most max bytes, returning a typed 413-class error when
// the source exceeds the ceiling (io.LimitReader bounds allocation; the
// excess probe is a single byte, so no full-buffer OOM occurs).
func ReadLimited(r io.Reader, max int64, authority string) ([]byte, error) {
	probe := io.LimitReader(r, max+1)
	data, err := io.ReadAll(probe)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, NewPayloadTooLargeError(max, authority)
	}
	return data, nil
}
