// Package client is the official Go client for the AuroraBoot REST API.
//
// AuroraBoot is a self-hosted Kairos node manager. This package wraps
// the HTTP routes documented in /api/v1/openapi.yaml (also served at
// /api/docs when the server is running) with a small, typed interface
// so third-party automation and the integration tests can talk to a
// running instance without open-coding fetches.
//
// Basic usage:
//
//	cli := client.New("http://auroraboot.local:8080",
//	    client.WithAdminPassword("s3cret"))
//	nodes, err := cli.Nodes.List(ctx, nil)
//
// Agent usage (for a freshly-booted Kairos node phoning home):
//
//	reg, err := cli.Nodes.Register(ctx, client.NodeRegisterRequest{
//	    RegistrationToken: token,
//	    MachineID:         machineID,
//	    Hostname:          hostname,
//	})
//	cli = cli.WithNodeAPIKey(reg.APIKey)
//	err = cli.Nodes.Heartbeat(ctx, reg.ID, client.NodeHeartbeatRequest{
//	    AgentVersion: "v2.27.0",
//	})
//
// Builds never reference files on the server by path. Overlay files are
// uploaded first and then referenced by ID. The ISO of a Trusted Boot
// build is the signed UKI installer, signed with a key set stored on the
// server and selected by its ID:
//
//	overlayID, err := cli.Artifacts.UploadOverlay(ctx,
//	    client.OverlayFile{Name: "overlay.tar.gz", Content: archive})
//	art, err := cli.Artifacts.Create(ctx, client.CreateArtifactRequest{
//	    BaseImage: baseImage, // e.g. "quay.io/kairos/ubuntu:<tag>"
//	    OverlayID: overlayID,
//	    Outputs:   client.ArtifactOutputs{ISO: true, TrustedBoot: true},
//	    Signing:   client.ArtifactSigning{UKIKeySetID: keySet.ID},
//	})
//
// Every method takes a context.Context so callers control
// cancellation and timeouts. Errors surfaced by the server (4xx/5xx)
// are returned as *APIError with the parsed error body and raw HTTP
// status code.
package client
