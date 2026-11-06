#!/bin/sh
# Builds two synthetic releases on top of the one the node is running, and
# the diffs between all three, for the diff-upgrade suite.
#
# Release 1 is the installed image itself: a non recursive bind mount of /
# shows the image alone, without the overlays and partitions mounted on
# top of it. Releases 2 and 3 are overlay mounts stacked on it, so every
# file they do not change keeps the owner, mode and extended attributes of
# the image, as it would in a real release built from the same base.
#
# Release 2 adds a 20 MiB blob, a file and a line in /etc/kairos-release.
# Release 3 drops the blob and the file of release 2, and adds its own blob
# of 35 MiB. The blobs are random so compression does not hide their size,
# and of different sizes so each diff has a size of its own: about 20 MiB
# for 1 to 2, and about 35 MiB for 2 to 3 and for 1 to 3.
#
# Also leaves $W/not-an-image.tar, the full image source the suite passes
# so that falling back to the full image fails.
#
# With a repository as its argument, it also publishes the diffs to 3 from
# both 1 and 2 to the index at <repository>:r3.diff, where
# "upgrade --diff --source oci:<repository>:r3" looks for them.
#
# Prints "SIZE <name> <bytes>" for each archive, and leaves the diffs in
# $W/d12.tar, $W/d23.tar and $W/d13.tar. full-2.tar is the whole of
# release 2 in the same format, as a stand in for the full image.
set -e

REPO=$1
W=/usr/local/diff-test
T=usr/share/kairos-diff-test

mkdir -p "$W/img1" "$W/img2" "$W/img3" "$W/up2" "$W/up3" "$W/work2" "$W/work3" "$W/empty/etc"
mount -o bind / "$W/img1"
mount -t overlay overlay -o "lowerdir=$W/img1,upperdir=$W/up2,workdir=$W/work2" "$W/img2"

echo 'KAIROS_DIFF_TEST="2"' >> "$W/img2/etc/kairos-release"
mkdir -p "$W/img2/$T"
echo two > "$W/img2/$T/release"
echo only-in-2 > "$W/img2/$T/only-in-2"
dd if=/dev/urandom of="$W/img2/$T/blob-2" bs=1M count=20 2>/dev/null
# A second name for the blob, which a diff has to keep a hard link.
ln "$W/img2/$T/blob-2" "$W/img2/$T/blob-2-link"

# The layers of release 2 rather than its mount, so the stack stays one
# overlay deep however / itself is mounted. Release 2 is not changed from
# here on, which a lower layer requires.
mount -t overlay overlay -o "lowerdir=$W/up2:$W/img1,upperdir=$W/up3,workdir=$W/work3" "$W/img3"

sed -i 's/^KAIROS_DIFF_TEST="2"$/KAIROS_DIFF_TEST="3"/' "$W/img3/etc/kairos-release"
echo three > "$W/img3/$T/release"
rm "$W/img3/$T/only-in-2" "$W/img3/$T/blob-2" "$W/img3/$T/blob-2-link"
echo only-in-3 > "$W/img3/$T/only-in-3"
dd if=/dev/urandom of="$W/img3/$T/blob-3" bs=1M count=35 2>/dev/null

echo 'not an image' > "$W/not-an-image.tar"

# Anything that is not one of the three releases is a base nothing matches,
# so diffing against it carries the whole target.
echo 'NOT_A_RELEASE=1' > "$W/empty/etc/kairos-release"

kairos-agent generate-diff --work-dir "$W" --output "$W/full-2.tar" "dir:$W/empty" "dir:$W/img2"
kairos-agent generate-diff --work-dir "$W" --output "$W/d12.tar" "dir:$W/img1" "dir:$W/img2"
kairos-agent generate-diff --work-dir "$W" --output "$W/d23.tar" "dir:$W/img2" "dir:$W/img3"
kairos-agent generate-diff --work-dir "$W" --output "$W/d13.tar" "dir:$W/img1" "dir:$W/img3"

if [ -n "$REPO" ]; then
	kairos-agent generate-diff --work-dir "$W" --push "$REPO:r3.diff" "dir:$W/img1" "dir:$W/img3"
	kairos-agent generate-diff --work-dir "$W" --push "$REPO:r3.diff" "dir:$W/img2" "dir:$W/img3"
fi

# Release 1 stays mounted: the suite upgrades to it from there, so the node
# records the ID of a copy of it that nothing has touched yet.
umount "$W/img3" "$W/img2"

for name in full-2 d12 d23 d13; do
	echo "SIZE $name $(stat -c %s "$W/$name.tar")"
done
