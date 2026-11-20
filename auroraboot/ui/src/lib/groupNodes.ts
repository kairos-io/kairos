import type { Group } from "@/api/groups";
import type { Node } from "@/api/nodes";

// groupNodeCount is how many nodes a group holds.
//
// It counts the node list the page already has, which is the source the board
// and the Health column count from too, so a move made on the page is reflected
// without a reload. `group.node_count` is the server's count, read once when
// the page loaded, and it stands in only while that list is not in hand: before
// the first answer arrives, and if the request for it failed. A server that
// does not send a count leaves the answer unknown.
export function groupNodeCount(group: Group, nodes: Node[], nodesLoaded: boolean): number | undefined {
  if (nodesLoaded) return nodes.filter((n) => n.groupID === group.id).length;
  return group.node_count;
}

// deleteGroupDescription is the sentence confirming a group delete. Deleting a
// group does not delete its nodes, so it says what becomes of them, and it
// never calls a group empty on a count that is not known.
export function deleteGroupDescription(name: string, count: number | undefined): string {
  const head = `Delete "${name}"?`;
  if (count === undefined) {
    return `${head} Any nodes in it will be moved out of this group (they stay registered).`;
  }
  if (count === 0) return `${head} This group has no nodes.`;
  return `${head} ${count} node(s) will be moved out of this group (they stay registered).`;
}
