package sync

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/v5/async/abool"
	mock "github.com/prysmaticlabs/prysm/v5/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/core/signing"
	dbtest "github.com/prysmaticlabs/prysm/v5/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/operations/attestations"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/p2p/peers"
	p2ptest "github.com/prysmaticlabs/prysm/v5/beacon-chain/p2p/testing"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/startup"
	lruwrpr "github.com/prysmaticlabs/prysm/v5/cache/lru"
	fieldparams "github.com/prysmaticlabs/prysm/v5/config/fieldparams"
	"github.com/prysmaticlabs/prysm/v5/config/params"
	"github.com/prysmaticlabs/prysm/v5/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	"github.com/prysmaticlabs/prysm/v5/encoding/bytesutil"
	ethpb "github.com/prysmaticlabs/prysm/v5/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/v5/proto/prysm/v1alpha1/attestation"
	"github.com/prysmaticlabs/prysm/v5/testing/assert"
	"github.com/prysmaticlabs/prysm/v5/testing/require"
	"github.com/prysmaticlabs/prysm/v5/testing/util"
	prysmTime "github.com/prysmaticlabs/prysm/v5/time"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func TestProcessPendingAtts_NoBlockRequestBlock(t *testing.T) {
	hook := logTest.NewGlobal()
	db := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	p2 := p2ptest.NewTestP2P(t)
	p1.Connect(p2)
	assert.Equal(t, 1, len(p1.BHost.Network().Peers()), "Expected peers to be connected")
	p1.Peers().Add(new(enr.Record), p2.PeerID(), nil, network.DirOutbound)
	p1.Peers().SetConnectionState(p2.PeerID(), peers.Connected)
	p1.Peers().SetChainState(p2.PeerID(), &ethpb.Status{})

	chain := &mock.ChainService{Genesis: prysmTime.Now(), FinalizedCheckPoint: &ethpb.Checkpoint{}}
	r := &Service{
		cfg:                  &config{p2p: p1, beaconDB: db, chain: chain, clock: startup.NewClock(chain.Genesis, chain.ValidatorsRoot)},
		blkRootToPendingAtts: make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
		chainStarted:         abool.New(),
	}

	a := &ethpb.AggregateAttestationAndProof{Aggregate: &ethpb.Attestation{Data: &ethpb.AttestationData{Target: &ethpb.Checkpoint{Root: make([]byte, 32)}}}}
	r.blkRootToPendingAtts[[32]byte{'A'}] = []ethpb.SignedAggregateAttAndProof{&ethpb.SignedAggregateAttestationAndProof{Message: a}}
	require.NoError(t, r.processPendingAtts(context.Background()))
	require.LogsContain(t, hook, "Requesting block by root")
}

func TestProcessPendingAtts_HasBlockSaveUnAggregatedAtt(t *testing.T) {
	hook := logTest.NewGlobal()
	db := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	validators := uint64(256)

	beaconState, privKeys := util.DeterministicGenesisState(t, validators)

	sb := util.NewBeaconBlock()
	util.SaveBlock(t, context.Background(), db, sb)
	root, err := sb.Block.HashTreeRoot()
	require.NoError(t, err)

	aggBits := bitfield.NewBitlist(8)
	aggBits.SetBitAt(1, true)
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			BeaconBlockRoot: root[:],
			Source:          &ethpb.Checkpoint{Epoch: 0, Root: bytesutil.PadTo([]byte("hello-world"), 32)},
			Target:          &ethpb.Checkpoint{Epoch: 0, Root: root[:]},
		},
		AggregationBits: aggBits,
	}

	committee, err := helpers.BeaconCommitteeFromState(context.Background(), beaconState, att.Data.Slot, att.Data.CommitteeIndex)
	assert.NoError(t, err)
	attestingIndices, err := attestation.AttestingIndices(att, committee)
	require.NoError(t, err)
	attesterDomain, err := signing.Domain(beaconState.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, beaconState.GenesisValidatorsRoot())
	require.NoError(t, err)
	hashTreeRoot, err := signing.ComputeSigningRoot(att.Data, attesterDomain)
	assert.NoError(t, err)
	for _, i := range attestingIndices {
		att.Signature = privKeys[i].Sign(hashTreeRoot[:]).Marshal()
	}

	aggregateAndProof := &ethpb.AggregateAttestationAndProof{
		Aggregate: att,
	}

	require.NoError(t, beaconState.SetGenesisTime(uint64(time.Now().Unix())))

	chain := &mock.ChainService{Genesis: time.Now(),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Root:  aggregateAndProof.Aggregate.Data.BeaconBlockRoot,
			Epoch: 0,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Service{
		ctx: ctx,
		cfg: &config{
			p2p:      p1,
			beaconDB: db,
			chain:    chain,
			clock:    startup.NewClock(chain.Genesis, chain.ValidatorsRoot),
			attPool:  attestations.NewPool(),
		},
		blkRootToPendingAtts:             make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
		seenUnAggregatedAttestationCache: lruwrpr.New(10),
		signatureChan:                    make(chan *signatureVerifier, verifierLimit),
	}
	go r.verifierRoutine()

	s, err := util.NewBeaconState()
	require.NoError(t, err)
	require.NoError(t, r.cfg.beaconDB.SaveState(context.Background(), s, root))

	r.blkRootToPendingAtts[root] = []ethpb.SignedAggregateAttAndProof{&ethpb.SignedAggregateAttestationAndProof{Message: aggregateAndProof}}
	require.NoError(t, r.processPendingAtts(context.Background()))

	atts, err := r.cfg.attPool.UnaggregatedAttestations()
	require.NoError(t, err)
	assert.Equal(t, 1, len(atts), "Did not save unaggregated att")
	assert.DeepEqual(t, att, atts[0], "Incorrect saved att")
	assert.Equal(t, 0, len(r.cfg.attPool.AggregatedAttestations()), "Did save aggregated att")
	require.LogsContain(t, hook, "Verified and saved pending attestations to pool")
	cancel()
}

func TestProcessPendingAtts_HasBlockSaveUnAggregatedAttElectra(t *testing.T) {
	hook := logTest.NewGlobal()
	db := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	validators := uint64(256)

	beaconState, privKeys := util.DeterministicGenesisStateElectra(t, validators)

	sb := util.NewBeaconBlockElectra()
	util.SaveBlock(t, context.Background(), db, sb)
	root, err := sb.Block.HashTreeRoot()
	require.NoError(t, err)

	att := &ethpb.SingleAttestation{
		Data: &ethpb.AttestationData{
			BeaconBlockRoot: root[:],
			Source:          &ethpb.Checkpoint{Epoch: 0, Root: bytesutil.PadTo([]byte("hello-world"), 32)},
			Target:          &ethpb.Checkpoint{Epoch: 0, Root: root[:]},
		},
	}
	aggregateAndProof := &ethpb.AggregateAttestationAndProofSingle{
		Aggregate: att,
	}

	committee, err := helpers.BeaconCommitteeFromState(context.Background(), beaconState, att.Data.Slot, att.Data.CommitteeIndex)
	assert.NoError(t, err)
	att.AttesterIndex = committee[0]
	attesterDomain, err := signing.Domain(beaconState.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, beaconState.GenesisValidatorsRoot())
	require.NoError(t, err)
	hashTreeRoot, err := signing.ComputeSigningRoot(att.Data, attesterDomain)
	assert.NoError(t, err)
	att.Signature = privKeys[committee[0]].Sign(hashTreeRoot[:]).Marshal()

	require.NoError(t, beaconState.SetGenesisTime(uint64(time.Now().Unix())))

	chain := &mock.ChainService{Genesis: time.Now(),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Root:  aggregateAndProof.Aggregate.Data.BeaconBlockRoot,
			Epoch: 0,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Service{
		ctx: ctx,
		cfg: &config{
			p2p:      p1,
			beaconDB: db,
			chain:    chain,
			clock:    startup.NewClock(chain.Genesis, chain.ValidatorsRoot),
			attPool:  attestations.NewPool(),
		},
		blkRootToPendingAtts:             make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
		seenUnAggregatedAttestationCache: lruwrpr.New(10),
		signatureChan:                    make(chan *signatureVerifier, verifierLimit),
	}
	go r.verifierRoutine()

	s, err := util.NewBeaconStateElectra()
	require.NoError(t, err)
	require.NoError(t, r.cfg.beaconDB.SaveState(context.Background(), s, root))

	r.blkRootToPendingAtts[root] = []ethpb.SignedAggregateAttAndProof{&ethpb.SignedAggregateAttestationAndProofSingle{Message: aggregateAndProof}}
	require.NoError(t, r.processPendingAtts(context.Background()))

	atts, err := r.cfg.attPool.UnaggregatedAttestations()
	require.NoError(t, err)
	require.Equal(t, 1, len(atts), "Did not save unaggregated att")
	assert.DeepEqual(t, att.ToAttestationElectra(committee), atts[0], "Incorrect saved att")
	assert.Equal(t, 0, len(r.cfg.attPool.AggregatedAttestations()), "Did save aggregated att")
	require.LogsContain(t, hook, "Verified and saved pending attestations to pool")
	cancel()
}

func TestProcessPendingAtts_NoBroadcastWithBadSignature(t *testing.T) {
	db := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)

	s, _ := util.DeterministicGenesisState(t, 256)
	chain := &mock.ChainService{
		State:   s,
		Genesis: prysmTime.Now(), FinalizedCheckPoint: &ethpb.Checkpoint{Root: make([]byte, 32)}}
	r := &Service{
		cfg: &config{
			p2p:      p1,
			beaconDB: db,
			chain:    chain,
			clock:    startup.NewClock(chain.Genesis, chain.ValidatorsRoot),
			attPool:  attestations.NewPool(),
		},
		blkRootToPendingAtts: make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
	}

	priv, err := bls.RandKey()
	require.NoError(t, err)
	a := &ethpb.AggregateAttestationAndProof{
		Aggregate: &ethpb.Attestation{
			Signature:       priv.Sign([]byte("foo")).Marshal(),
			AggregationBits: bitfield.Bitlist{0x02},
			Data:            util.HydrateAttestationData(&ethpb.AttestationData{}),
		},
		SelectionProof: make([]byte, fieldparams.BLSSignatureLength),
	}

	b := util.NewBeaconBlock()
	r32, err := b.Block.HashTreeRoot()
	require.NoError(t, err)
	util.SaveBlock(t, context.Background(), r.cfg.beaconDB, b)
	require.NoError(t, r.cfg.beaconDB.SaveState(context.Background(), s, r32))

	r.blkRootToPendingAtts[r32] = []ethpb.SignedAggregateAttAndProof{&ethpb.SignedAggregateAttestationAndProof{Message: a, Signature: make([]byte, fieldparams.BLSSignatureLength)}}
	require.NoError(t, r.processPendingAtts(context.Background()))

	assert.Equal(t, false, p1.BroadcastCalled.Load(), "Broadcasted bad aggregate")
	// Clear pool.
	err = r.cfg.attPool.DeleteUnaggregatedAttestation(a.Aggregate)
	require.NoError(t, err)

	validators := uint64(256)

	_, privKeys := util.DeterministicGenesisState(t, validators)
	aggBits := bitfield.NewBitlist(8)
	aggBits.SetBitAt(1, true)
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			BeaconBlockRoot: r32[:],
			Source:          &ethpb.Checkpoint{Epoch: 0, Root: bytesutil.PadTo([]byte("hello-world"), 32)},
			Target:          &ethpb.Checkpoint{Epoch: 0, Root: r32[:]},
		},
		AggregationBits: aggBits,
	}
	committee, err := helpers.BeaconCommitteeFromState(context.Background(), s, att.Data.Slot, att.Data.CommitteeIndex)
	assert.NoError(t, err)
	attestingIndices, err := attestation.AttestingIndices(att, committee)
	require.NoError(t, err)
	attesterDomain, err := signing.Domain(s.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, s.GenesisValidatorsRoot())
	require.NoError(t, err)
	hashTreeRoot, err := signing.ComputeSigningRoot(att.Data, attesterDomain)
	assert.NoError(t, err)
	for _, i := range attestingIndices {
		att.Signature = privKeys[i].Sign(hashTreeRoot[:]).Marshal()
	}

	// Arbitrary aggregator index for testing purposes.
	aggregatorIndex := committee[0]
	sszSlot := primitives.SSZUint64(att.Data.Slot)
	sig, err := signing.ComputeDomainAndSign(s, 0, &sszSlot, params.BeaconConfig().DomainSelectionProof, privKeys[aggregatorIndex])
	require.NoError(t, err)
	aggregateAndProof := &ethpb.AggregateAttestationAndProof{
		SelectionProof:  sig,
		Aggregate:       att,
		AggregatorIndex: aggregatorIndex,
	}
	aggreSig, err := signing.ComputeDomainAndSign(s, 0, aggregateAndProof, params.BeaconConfig().DomainAggregateAndProof, privKeys[aggregatorIndex])
	require.NoError(t, err)

	require.NoError(t, s.SetGenesisTime(uint64(time.Now().Unix())))
	ctx, cancel := context.WithCancel(context.Background())
	chain2 := &mock.ChainService{Genesis: time.Now(),
		State: s,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Root:  aggregateAndProof.Aggregate.Data.BeaconBlockRoot,
			Epoch: 0,
		}}
	r = &Service{
		ctx: ctx,
		cfg: &config{
			p2p:      p1,
			beaconDB: db,
			chain:    chain2,
			clock:    startup.NewClock(chain2.Genesis, chain2.ValidatorsRoot),
			attPool:  attestations.NewPool(),
		},
		blkRootToPendingAtts:             make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
		seenUnAggregatedAttestationCache: lruwrpr.New(10),
		signatureChan:                    make(chan *signatureVerifier, verifierLimit),
	}
	go r.verifierRoutine()

	r.blkRootToPendingAtts[r32] = []ethpb.SignedAggregateAttAndProof{&ethpb.SignedAggregateAttestationAndProof{Message: aggregateAndProof, Signature: aggreSig}}
	require.NoError(t, r.processPendingAtts(context.Background()))

	assert.Equal(t, true, p1.BroadcastCalled.Load(), "Could not broadcast the good aggregate")
	cancel()
}

func TestProcessPendingAtts_HasBlockSaveAggregatedAtt(t *testing.T) {
	hook := logTest.NewGlobal()
	db := dbtest.SetupDB(t)
	p1 := p2ptest.NewTestP2P(t)
	validators := uint64(256)

	beaconState, privKeys := util.DeterministicGenesisState(t, validators)

	sb := util.NewBeaconBlock()
	util.SaveBlock(t, context.Background(), db, sb)
	root, err := sb.Block.HashTreeRoot()
	require.NoError(t, err)

	aggBits := bitfield.NewBitlist(validators / uint64(params.BeaconConfig().SlotsPerEpoch))
	aggBits.SetBitAt(0, true)
	aggBits.SetBitAt(1, true)
	att := &ethpb.Attestation{
		Data: &ethpb.AttestationData{
			BeaconBlockRoot: root[:],
			Source:          &ethpb.Checkpoint{Epoch: 0, Root: bytesutil.PadTo([]byte("hello-world"), 32)},
			Target:          &ethpb.Checkpoint{Epoch: 0, Root: root[:]},
		},
		AggregationBits: aggBits,
	}

	committee, err := helpers.BeaconCommitteeFromState(context.Background(), beaconState, att.Data.Slot, att.Data.CommitteeIndex)
	assert.NoError(t, err)
	attestingIndices, err := attestation.AttestingIndices(att, committee)
	require.NoError(t, err)
	attesterDomain, err := signing.Domain(beaconState.Fork(), 0, params.BeaconConfig().DomainBeaconAttester, beaconState.GenesisValidatorsRoot())
	require.NoError(t, err)
	hashTreeRoot, err := signing.ComputeSigningRoot(att.Data, attesterDomain)
	assert.NoError(t, err)
	sigs := make([]bls.Signature, len(attestingIndices))
	for i, indice := range attestingIndices {
		sig := privKeys[indice].Sign(hashTreeRoot[:])
		sigs[i] = sig
	}
	att.Signature = bls.AggregateSignatures(sigs).Marshal()

	// Arbitrary aggregator index for testing purposes.
	aggregatorIndex := committee[0]
	sszUint := primitives.SSZUint64(att.Data.Slot)
	sig, err := signing.ComputeDomainAndSign(beaconState, 0, &sszUint, params.BeaconConfig().DomainSelectionProof, privKeys[aggregatorIndex])
	require.NoError(t, err)
	aggregateAndProof := &ethpb.AggregateAttestationAndProof{
		SelectionProof:  sig,
		Aggregate:       att,
		AggregatorIndex: aggregatorIndex,
	}
	aggreSig, err := signing.ComputeDomainAndSign(beaconState, 0, aggregateAndProof, params.BeaconConfig().DomainAggregateAndProof, privKeys[aggregatorIndex])
	require.NoError(t, err)

	require.NoError(t, beaconState.SetGenesisTime(uint64(time.Now().Unix())))

	chain := &mock.ChainService{Genesis: time.Now(),
		DB:    db,
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Root:  aggregateAndProof.Aggregate.Data.BeaconBlockRoot,
			Epoch: 0,
		}}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Service{
		ctx: ctx,
		cfg: &config{
			p2p:      p1,
			beaconDB: db,
			chain:    chain,
			clock:    startup.NewClock(chain.Genesis, chain.ValidatorsRoot),
			attPool:  attestations.NewPool(),
		},
		blkRootToPendingAtts:           make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
		seenAggregatedAttestationCache: lruwrpr.New(10),
		signatureChan:                  make(chan *signatureVerifier, verifierLimit),
	}
	go r.verifierRoutine()
	s, err := util.NewBeaconState()
	require.NoError(t, err)
	require.NoError(t, r.cfg.beaconDB.SaveState(context.Background(), s, root))

	r.blkRootToPendingAtts[root] = []ethpb.SignedAggregateAttAndProof{&ethpb.SignedAggregateAttestationAndProof{Message: aggregateAndProof, Signature: aggreSig}}
	require.NoError(t, r.processPendingAtts(context.Background()))

	assert.Equal(t, 1, len(r.cfg.attPool.AggregatedAttestations()), "Did not save aggregated att")
	assert.DeepEqual(t, att, r.cfg.attPool.AggregatedAttestations()[0], "Incorrect saved att")
	atts, err := r.cfg.attPool.UnaggregatedAttestations()
	require.NoError(t, err)
	assert.Equal(t, 0, len(atts), "Did save aggregated att")
	require.LogsContain(t, hook, "Verified and saved pending attestations to pool")
	cancel()
}

func TestValidatePendingAtts_CanPruneOldAtts(t *testing.T) {
	s := &Service{
		blkRootToPendingAtts: make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
	}

	// 100 Attestations per block root.
	r1 := [32]byte{'A'}
	r2 := [32]byte{'B'}
	r3 := [32]byte{'C'}

	for i := primitives.Slot(0); i < 100; i++ {
		s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				AggregatorIndex: primitives.ValidatorIndex(i),
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{Slot: i, BeaconBlockRoot: r1[:]}}}})
		s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				AggregatorIndex: primitives.ValidatorIndex(i*2 + i),
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{Slot: i, BeaconBlockRoot: r2[:]}}}})
		s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				AggregatorIndex: primitives.ValidatorIndex(i*3 + i),
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{Slot: i, BeaconBlockRoot: r3[:]}}}})
	}

	assert.Equal(t, 100, len(s.blkRootToPendingAtts[r1]), "Did not save pending atts")
	assert.Equal(t, 100, len(s.blkRootToPendingAtts[r2]), "Did not save pending atts")
	assert.Equal(t, 100, len(s.blkRootToPendingAtts[r3]), "Did not save pending atts")

	// Set current slot to 50, it should prune 19 attestations. (50 - 31)
	s.validatePendingAtts(context.Background(), 50)
	assert.Equal(t, 81, len(s.blkRootToPendingAtts[r1]), "Did not delete pending atts")
	assert.Equal(t, 81, len(s.blkRootToPendingAtts[r2]), "Did not delete pending atts")
	assert.Equal(t, 81, len(s.blkRootToPendingAtts[r3]), "Did not delete pending atts")

	// Set current slot to 100 + slot_duration, it should prune all the attestations.
	s.validatePendingAtts(context.Background(), 100+params.BeaconConfig().SlotsPerEpoch)
	assert.Equal(t, 0, len(s.blkRootToPendingAtts[r1]), "Did not delete pending atts")
	assert.Equal(t, 0, len(s.blkRootToPendingAtts[r2]), "Did not delete pending atts")
	assert.Equal(t, 0, len(s.blkRootToPendingAtts[r3]), "Did not delete pending atts")

	// Verify the keys are deleted.
	assert.Equal(t, 0, len(s.blkRootToPendingAtts), "Did not delete block keys")
}

func TestValidatePendingAtts_NoDuplicatingAtts(t *testing.T) {
	s := &Service{
		blkRootToPendingAtts: make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
	}

	r1 := [32]byte{'A'}
	r2 := [32]byte{'B'}
	s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
		Message: &ethpb.AggregateAttestationAndProof{
			AggregatorIndex: 1,
			Aggregate: &ethpb.Attestation{
				Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: r1[:]}}}})
	s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
		Message: &ethpb.AggregateAttestationAndProof{
			AggregatorIndex: 2,
			Aggregate: &ethpb.Attestation{
				Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: r2[:]}}}})
	s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
		Message: &ethpb.AggregateAttestationAndProof{
			AggregatorIndex: 2,
			Aggregate: &ethpb.Attestation{
				Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: r2[:]}}}})

	assert.Equal(t, 1, len(s.blkRootToPendingAtts[r1]), "Did not save pending atts")
	assert.Equal(t, 1, len(s.blkRootToPendingAtts[r2]), "Did not save pending atts")
}

func TestSavePendingAtts_BeyondLimit(t *testing.T) {
	s := &Service{
		blkRootToPendingAtts: make(map[[32]byte][]ethpb.SignedAggregateAttAndProof),
	}

	for i := 0; i < pendingAttsLimit; i++ {
		s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				AggregatorIndex: primitives.ValidatorIndex(i),
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: bytesutil.Bytes32(uint64(i))}}}})
	}
	r1 := [32]byte(bytesutil.Bytes32(0))
	r2 := [32]byte(bytesutil.Bytes32(uint64(pendingAttsLimit) - 1))

	assert.Equal(t, 1, len(s.blkRootToPendingAtts[r1]), "Did not save pending atts")
	assert.Equal(t, 1, len(s.blkRootToPendingAtts[r2]), "Did not save pending atts")

	for i := pendingAttsLimit; i < pendingAttsLimit+20; i++ {
		s.savePendingAtt(&ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				AggregatorIndex: primitives.ValidatorIndex(i),
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: bytesutil.Bytes32(uint64(i))}}}})
	}

	r1 = [32]byte(bytesutil.Bytes32(uint64(pendingAttsLimit)))
	r2 = [32]byte(bytesutil.Bytes32(uint64(pendingAttsLimit) + 10))

	assert.Equal(t, 0, len(s.blkRootToPendingAtts[r1]), "Saved pending atts")
	assert.Equal(t, 0, len(s.blkRootToPendingAtts[r2]), "Saved pending atts")
}

func Test_attsAreEqual_Committee(t *testing.T) {
	t.Run("Phase 0 equal", func(t *testing.T) {
		att1 := &ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{
						CommitteeIndex: 0}}}}
		att2 := &ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{
						CommitteeIndex: 0}}}}
		assert.Equal(t, true, attsAreEqual(att1, att2))
	})
	t.Run("Phase 0 not equal", func(t *testing.T) {
		att1 := &ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{
						CommitteeIndex: 0}}}}
		att2 := &ethpb.SignedAggregateAttestationAndProof{
			Message: &ethpb.AggregateAttestationAndProof{
				Aggregate: &ethpb.Attestation{
					Data: &ethpb.AttestationData{
						CommitteeIndex: 1}}}}
		assert.Equal(t, false, attsAreEqual(att1, att2))
	})
	t.Run("Electra equal", func(t *testing.T) {
		cb1 := primitives.NewAttestationCommitteeBits()
		cb1.SetBitAt(0, true)
		att1 := &ethpb.SignedAggregateAttestationAndProofElectra{
			Message: &ethpb.AggregateAttestationAndProofElectra{
				Aggregate: &ethpb.AttestationElectra{
					Data:          &ethpb.AttestationData{},
					CommitteeBits: cb1,
				}}}
		cb2 := primitives.NewAttestationCommitteeBits()
		cb2.SetBitAt(0, true)
		att2 := &ethpb.SignedAggregateAttestationAndProofElectra{
			Message: &ethpb.AggregateAttestationAndProofElectra{
				Aggregate: &ethpb.AttestationElectra{
					Data:          &ethpb.AttestationData{},
					CommitteeBits: cb2,
				}}}
		assert.Equal(t, true, attsAreEqual(att1, att2))
	})
	t.Run("Electra not equal", func(t *testing.T) {
		cb1 := primitives.NewAttestationCommitteeBits()
		cb1.SetBitAt(0, true)
		att1 := &ethpb.SignedAggregateAttestationAndProofElectra{
			Message: &ethpb.AggregateAttestationAndProofElectra{
				Aggregate: &ethpb.AttestationElectra{
					Data:          &ethpb.AttestationData{},
					CommitteeBits: cb1,
				}}}
		cb2 := primitives.NewAttestationCommitteeBits()
		cb2.SetBitAt(1, true)
		att2 := &ethpb.SignedAggregateAttestationAndProofElectra{
			Message: &ethpb.AggregateAttestationAndProofElectra{
				Aggregate: &ethpb.AttestationElectra{
					Data:          &ethpb.AttestationData{},
					CommitteeBits: cb2,
				}}}
		assert.Equal(t, false, attsAreEqual(att1, att2))
	})
	t.Run("Single and Electra not equal", func(t *testing.T) {
		cb := primitives.NewAttestationCommitteeBits()
		cb.SetBitAt(0, true)
		att1 := &ethpb.SignedAggregateAttestationAndProofElectra{
			Message: &ethpb.AggregateAttestationAndProofElectra{
				Aggregate: &ethpb.AttestationElectra{
					Data:          &ethpb.AttestationData{},
					CommitteeBits: cb,
				}}}
		att2 := &ethpb.SignedAggregateAttestationAndProofSingle{
			Message: &ethpb.AggregateAttestationAndProofSingle{
				Aggregate: &ethpb.SingleAttestation{
					CommitteeId:   0,
					AttesterIndex: 0,
					Data:          &ethpb.AttestationData{},
				},
			},
		}
		assert.Equal(t, false, attsAreEqual(att1, att2))
	})
	t.Run("Single equal", func(t *testing.T) {
		att1 := &ethpb.SignedAggregateAttestationAndProofSingle{
			Message: &ethpb.AggregateAttestationAndProofSingle{
				Aggregate: &ethpb.SingleAttestation{
					CommitteeId:   0,
					AttesterIndex: 0,
					Data:          &ethpb.AttestationData{},
				},
			},
		}
		att2 := &ethpb.SignedAggregateAttestationAndProofSingle{
			Message: &ethpb.AggregateAttestationAndProofSingle{
				Aggregate: &ethpb.SingleAttestation{
					CommitteeId:   0,
					AttesterIndex: 0,
					Data:          &ethpb.AttestationData{},
				},
			},
		}
		assert.Equal(t, true, attsAreEqual(att1, att2))
	})
	t.Run("Single not equal", func(t *testing.T) {
		// Same AttesterIndex but different CommitteeId
		att1 := &ethpb.SignedAggregateAttestationAndProofSingle{
			Message: &ethpb.AggregateAttestationAndProofSingle{
				Aggregate: &ethpb.SingleAttestation{
					CommitteeId:   0,
					AttesterIndex: 0,
					Data:          &ethpb.AttestationData{},
				},
			},
		}
		att2 := &ethpb.SignedAggregateAttestationAndProofSingle{
			Message: &ethpb.AggregateAttestationAndProofSingle{
				Aggregate: &ethpb.SingleAttestation{
					CommitteeId:   1,
					AttesterIndex: 0,
					Data:          &ethpb.AttestationData{},
				},
			},
		}
		assert.Equal(t, false, attsAreEqual(att1, att2))

		// Same CommitteeId but different AttesterIndex
		att2.Message.Aggregate.CommitteeId = 0
		att2.Message.Aggregate.AttesterIndex = 1
		assert.Equal(t, false, attsAreEqual(att1, att2))
	})
}
