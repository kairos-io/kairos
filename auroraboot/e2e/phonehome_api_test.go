package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The helpers below drive the AuroraBoot admin API for the phonehome VM tier.
// Each one fails the spec on a status it does not expect, so a spec reads as
// the flow it checks rather than as HTTP plumbing.

// createGroup creates a node group and returns its ID.
func (w *webServer) createGroup(name string) string {
	resp, data := w.do(http.MethodPost, "/api/v1/groups", map[string]any{"name": name})
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusCreated), string(data))
	var group struct {
		ID string `json:"id"`
	}
	ExpectWithOffset(1, json.Unmarshal(data, &group)).To(Succeed())
	ExpectWithOffset(1, group.ID).ToNot(BeEmpty(), string(data))
	return group.ID
}

// buildArtifact starts an artifact build and waits up to 20 minutes for it to
// be Ready. A build that ends in Error fails the spec with its build log
// attached to the report.
func (w *webServer) buildArtifact(body map[string]any) string {
	resp, data := w.do(http.MethodPost, "/api/v1/artifacts", body)
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusCreated), string(data))
	var created struct {
		ID string `json:"id"`
	}
	ExpectWithOffset(1, json.Unmarshal(data, &created)).To(Succeed())
	ExpectWithOffset(1, created.ID).ToNot(BeEmpty(), string(data))
	id := created.ID

	var phase, message string
	EventuallyWithOffset(1, func() string {
		_, d := w.do(http.MethodGet, "/api/v1/artifacts/"+id, nil)
		var rec struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(d, &rec)
		phase, message = rec.Phase, rec.Message
		return phase
	}, "20m", "10s").Should(BeElementOf("Ready", "Error"),
		fmt.Sprintf("artifact %s (%v) did not finish building within 20 minutes", id, body["name"]))

	if phase != "Ready" {
		_, logs := w.do(http.MethodGet, "/api/v1/artifacts/"+id+"/logs", nil)
		AddReportEntry(fmt.Sprintf("build log of artifact %s (%v)", id, body["name"]), string(logs))
	}
	ExpectWithOffset(1, phase).To(Equal("Ready"),
		fmt.Sprintf("artifact %s (%v) failed to build: %s", id, body["name"], message))
	return id
}

// downloadISO saves the artifact's ISO into dir and returns its path. The ISO
// is streamed to disk, since it is too large to hold in memory comfortably.
func (w *webServer) downloadISO(artifactID, dir string) string {
	_, data := w.do(http.MethodGet, "/api/v1/artifacts/"+artifactID, nil)
	var rec struct {
		Artifacts []string `json:"artifacts"`
	}
	ExpectWithOffset(1, json.Unmarshal(data, &rec)).To(Succeed())
	var iso string
	for _, f := range rec.Artifacts {
		if strings.HasSuffix(f, ".iso") {
			iso = filepath.Base(f)
		}
	}
	ExpectWithOffset(1, iso).ToNot(BeEmpty(), "artifact %s lists no ISO: %s", artifactID, string(data))

	req, err := http.NewRequest(http.MethodGet, w.baseURL+"/api/v1/artifacts/"+artifactID+"/download/"+iso, nil)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+webAdminPassword)
	resp, err := http.DefaultClient.Do(req)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		Fail(fmt.Sprintf("downloading %s of artifact %s: status %d: %s", iso, artifactID, resp.StatusCode, body), 1)
	}

	path := filepath.Join(dir, iso)
	f, err := os.Create(path)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "downloading %s of artifact %s", iso, artifactID)
	return path
}

// groupNodes lists the nodes in a group.
func (w *webServer) groupNodes(groupID string) []map[string]any {
	resp, data := w.do(http.MethodGet, "/api/v1/nodes?group="+groupID, nil)
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusOK), string(data))
	var nodes []map[string]any
	ExpectWithOffset(1, json.Unmarshal(data, &nodes)).To(Succeed(), string(data))
	return nodes
}

// sendGroupCommand starts a batch of a command over every node in the group
// and returns the batch ID. rollout holds the batch settings sent next to the
// command, such as concurrency; nil runs every node at once.
func (w *webServer) sendGroupCommand(groupID, command string, args map[string]string, rollout map[string]any) string {
	body := map[string]any{"command": command, "args": args}
	for k, v := range rollout {
		body[k] = v
	}
	resp, data := w.do(http.MethodPost, "/api/v1/groups/"+groupID+"/commands", body)
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusCreated), string(data))
	var batch struct {
		ID string `json:"id"`
	}
	ExpectWithOffset(1, json.Unmarshal(data, &batch)).To(Succeed(), string(data))
	ExpectWithOffset(1, batch.ID).ToNot(BeEmpty(), string(data))
	return batch.ID
}

// batch returns a batch record with its node rows in rollout order.
func (w *webServer) batch(id string) map[string]any {
	resp, data := w.do(http.MethodGet, "/api/v1/batches/"+id, nil)
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusOK), string(data))
	var b map[string]any
	ExpectWithOffset(1, json.Unmarshal(data, &b)).To(Succeed(), string(data))
	return b
}

// batchNodes returns the node rows of a batch record.
func batchNodes(b map[string]any) []map[string]any {
	raw, _ := b["nodes"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		row, ok := r.(map[string]any)
		ExpectWithOffset(1, ok).To(BeTrue(), "batch node row %v", r)
		rows = append(rows, row)
	}
	return rows
}

// batchCommandIDs returns the IDs of the commands a batch created. A batch
// that runs every node at once has created a command for each node by the
// time the group endpoint returns.
func (w *webServer) batchCommandIDs(batchID string) []string {
	b := w.batch(batchID)
	var ids []string
	for _, row := range batchNodes(b) {
		id, _ := row["commandID"].(string)
		ExpectWithOffset(1, id).NotTo(BeEmpty(), "node %v has no command in batch %v", row["nodeID"], b)
		ids = append(ids, id)
	}
	return ids
}

// nodeCommand is a command as the admin API lists it for a node.
type nodeCommand struct {
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Result string `json:"result"`
}

// nodeCommands lists every command queued for a node.
func (w *webServer) nodeCommands(nodeID string) []nodeCommand {
	resp, data := w.do(http.MethodGet, "/api/v1/nodes/"+nodeID+"/commands", nil)
	ExpectWithOffset(2, resp.StatusCode).To(Equal(http.StatusOK), string(data))
	var cmds []nodeCommand
	ExpectWithOffset(2, json.Unmarshal(data, &cmds)).To(Succeed(), string(data))
	return cmds
}

// commandFor returns the one command of a node whose ID is among ids, which is
// how a group batch is matched to the node that received each command.
func (w *webServer) commandFor(nodeID string, ids []string) string {
	var found []string
	for _, c := range w.nodeCommands(nodeID) {
		for _, id := range ids {
			if c.ID == id {
				found = append(found, c.ID)
			}
		}
	}
	ExpectWithOffset(1, found).To(HaveLen(1), "commands %v sent to node %s", ids, nodeID)
	return found[0]
}

// commandPhase returns the phase of one command of a node.
func (w *webServer) commandPhase(nodeID, commandID string) string {
	return w.command(nodeID, commandID).Phase
}

// command returns one command of a node, failing the spec when the node has
// no command with that ID.
func (w *webServer) command(nodeID, commandID string) nodeCommand {
	for _, c := range w.nodeCommands(nodeID) {
		if c.ID == commandID {
			return c
		}
	}
	Fail(fmt.Sprintf("node %s has no command %s", nodeID, commandID), 2)
	return nodeCommand{}
}

// node returns a node record.
func (w *webServer) node(nodeID string) map[string]any {
	resp, data := w.do(http.MethodGet, "/api/v1/nodes/"+nodeID, nil)
	ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusOK), string(data))
	var n map[string]any
	ExpectWithOffset(1, json.Unmarshal(data, &n)).To(Succeed(), string(data))
	return n
}

// deleteArtifact removes an artifact, which also removes the container image
// AuroraBoot built for it. It is cleanup, so a failure is only printed.
func (w *webServer) deleteArtifact(id string) {
	req, err := http.NewRequest(http.MethodDelete, w.baseURL+"/api/v1/artifacts/"+id, nil)
	if err != nil {
		fmt.Printf("Could not delete artifact %s: %s\n", id, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+webAdminPassword)
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Could not delete artifact %s: %s\n", id, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		fmt.Printf("Could not delete artifact %s: status %d\n", id, resp.StatusCode)
	}
}
