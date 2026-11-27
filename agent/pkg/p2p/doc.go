// Package p2p is an EXPERIMENTAL, alpha-quality way for the nodes of a closed
// network to share their active, passive and recovery images and to fetch them
// from several peers at once. It only uses net/http: every seeder serves, read
// only, a manifest (chunk and image sha256) and byte ranges of the image, and
// a downloader fetches verified chunks in parallel from all known peers. A node
// that is still downloading re-serves the chunks it already holds.
//
// It is opt-in and meant for isolated, trusted networks. See the docs for the
// threat model.
package p2p
