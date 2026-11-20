package builder

// LogBroadcaster receives every log chunk produced by a build. Concrete
// builders call BroadcastLogChunk from their log-flush path so subscribed
// UI clients can render build output in real time. The production
// implementation is a thin adapter over pkg/ws.UIHub; tests pass a nil
// broadcaster (fall back to DB-only) or a slice-collecting fake.
//
// The interface lives here so every ArtifactBuilder implementation can
// reference it without importing another builder package (which would
// risk an import cycle).
type LogBroadcaster interface {
	BroadcastLogChunk(buildID string, chunk string)
}
