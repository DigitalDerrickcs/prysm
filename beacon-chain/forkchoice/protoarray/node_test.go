package protoarray

import (
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestNode_Getters(t *testing.T) {
	slot := uint64(100)
	root := [32]byte{'a'}
	parent := uint64(10)
	jEpoch := uint64(20)
	fEpoch := uint64(30)
	weight := uint64(10000)
	bestChild := uint64(5)
	bestDescendant := uint64(4)
	graffiti := [32]byte{'b'}
	n := &Node{
		slot:           slot,
		root:           root,
		parent:         parent,
		justifiedEpoch: jEpoch,
		finalizedEpoch: fEpoch,
		weight:         weight,
		bestChild:      bestChild,
		bestDescendant: bestDescendant,
		graffiti:       graffiti,
	}

	require.Equal(t, slot, n.Slot())
	require.Equal(t, root, n.Root())
	require.Equal(t, parent, n.Parent())
	require.Equal(t, jEpoch, n.JustifiedEpoch())
	require.Equal(t, fEpoch, n.FinalizedEpoch())
	require.Equal(t, weight, n.Weight())
	require.Equal(t, bestChild, n.BestChild())
	require.Equal(t, bestDescendant, n.BestDescendant())
	require.Equal(t, graffiti, n.Graffiti())
}
