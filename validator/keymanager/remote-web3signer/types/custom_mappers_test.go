package types_test

import (
	"reflect"
	"testing"

	"github.com/prysmaticlabs/go-bitfield"
	fieldparams "github.com/prysmaticlabs/prysm/v5/config/fieldparams"
	"github.com/prysmaticlabs/prysm/v5/consensus-types/primitives"
	ethpb "github.com/prysmaticlabs/prysm/v5/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/v5/validator/keymanager/remote-web3signer/types"
	"github.com/prysmaticlabs/prysm/v5/validator/keymanager/remote-web3signer/types/mock"
)

func TestMapAggregateAndProof(t *testing.T) {
	type args struct {
		from *ethpb.AggregateAttestationAndProof
	}
	tests := []struct {
		name    string
		args    args
		want    *types.AggregateAndProof
		wantErr bool
	}{
		{
			name: "HappyPathTest",
			args: args{
				from: &ethpb.AggregateAttestationAndProof{
					AggregatorIndex: 0,
					Aggregate: &ethpb.Attestation{
						AggregationBits: bitfield.Bitlist{0b1101},
						Data: &ethpb.AttestationData{
							BeaconBlockRoot: make([]byte, fieldparams.RootLength),
							Source: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
							Target: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
						},
						Signature: make([]byte, 96),
					},
					SelectionProof: make([]byte, fieldparams.BLSSignatureLength),
				},
			},
			want: &types.AggregateAndProof{
				AggregatorIndex: "0",
				Aggregate:       mock.Attestation(),
				SelectionProof:  make([]byte, fieldparams.BLSSignatureLength),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapAggregateAndProof(tt.args.from)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAggregateAndProof() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Aggregate, tt.want.Aggregate) {
				t.Errorf("MapAggregateAndProof() got = %v, want %v", got.Aggregate, tt.want.Aggregate)
			}
		})
	}
}

func TestMapAggregateAndProofElectra(t *testing.T) {
	type args struct {
		from *ethpb.AggregateAttestationAndProofElectra
	}
	tests := []struct {
		name    string
		args    args
		want    *types.AggregateAndProofElectra
		wantErr bool
	}{
		{
			name: "HappyPathTest",
			args: args{
				from: &ethpb.AggregateAttestationAndProofElectra{
					AggregatorIndex: 0,
					Aggregate: &ethpb.AttestationElectra{
						AggregationBits: bitfield.Bitlist{0b1101},
						Data: &ethpb.AttestationData{
							BeaconBlockRoot: make([]byte, fieldparams.RootLength),
							Source: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
							Target: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
						},
						Signature: make([]byte, 96),
						CommitteeBits: func() bitfield.Bitvector64 {
							committeeBits := bitfield.NewBitvector64()
							committeeBits.SetBitAt(0, true)
							return committeeBits
						}(),
					},
					SelectionProof: make([]byte, fieldparams.BLSSignatureLength),
				},
			},
			want: &types.AggregateAndProofElectra{
				AggregatorIndex: "0",
				Aggregate:       mock.AttestationElectra(),
				SelectionProof:  make([]byte, fieldparams.BLSSignatureLength),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapAggregateAndProofElectra(tt.args.from)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAggregateAndProof() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Aggregate, tt.want.Aggregate) {
				t.Errorf("MapAggregateAndProof() got = %v, want %v", got.Aggregate, tt.want.Aggregate)
			}
		})
	}
}

func TestMapAttestation(t *testing.T) {
	type args struct {
		attestation *ethpb.Attestation
	}
	tests := []struct {
		name    string
		args    args
		want    *types.Attestation
		wantErr bool
	}{
		{
			name: "HappyPathTest",
			args: args{
				attestation: &ethpb.Attestation{
					AggregationBits: bitfield.Bitlist{0b1101},
					Data: &ethpb.AttestationData{
						BeaconBlockRoot: make([]byte, fieldparams.RootLength),
						Source: &ethpb.Checkpoint{
							Root: make([]byte, fieldparams.RootLength),
						},
						Target: &ethpb.Checkpoint{
							Root: make([]byte, fieldparams.RootLength),
						},
					},
					Signature: make([]byte, 96),
				},
			},
			want:    mock.Attestation(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapAttestation(tt.args.attestation)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAttestation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapAttestation() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapAttestationElectra(t *testing.T) {
	type args struct {
		attestation *ethpb.AttestationElectra
	}
	tests := []struct {
		name    string
		args    args
		want    *types.AttestationElectra
		wantErr bool
	}{
		{
			name: "HappyPathTest",
			args: args{
				attestation: &ethpb.AttestationElectra{
					AggregationBits: bitfield.Bitlist{0b1101},
					Data: &ethpb.AttestationData{
						BeaconBlockRoot: make([]byte, fieldparams.RootLength),
						Source: &ethpb.Checkpoint{
							Root: make([]byte, fieldparams.RootLength),
						},
						Target: &ethpb.Checkpoint{
							Root: make([]byte, fieldparams.RootLength),
						},
					},
					CommitteeBits: func() bitfield.Bitvector64 {
						committeeBits := bitfield.NewBitvector64()
						committeeBits.SetBitAt(0, true)
						return committeeBits
					}(),
					Signature: make([]byte, 96),
				},
			},
			want:    mock.AttestationElectra(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapAttestationElectra(tt.args.attestation)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAttestation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapAttestation() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapAttestationData(t *testing.T) {
	type args struct {
		data *ethpb.AttestationData
	}
	tests := []struct {
		name    string
		args    args
		want    *types.AttestationData
		wantErr bool
	}{
		{
			name: "HappyPathTest",
			args: args{
				data: &ethpb.AttestationData{
					BeaconBlockRoot: make([]byte, fieldparams.RootLength),
					Source: &ethpb.Checkpoint{
						Root: make([]byte, fieldparams.RootLength),
					},
					Target: &ethpb.Checkpoint{
						Root: make([]byte, fieldparams.RootLength),
					},
				},
			},
			want:    mock.Attestation().Data,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapAttestationData(tt.args.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAttestationData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapAttestationData() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapAttesterSlashing(t *testing.T) {
	type args struct {
		slashing *ethpb.AttesterSlashing
	}
	tests := []struct {
		name    string
		args    args
		want    *types.AttesterSlashing
		wantErr bool
	}{
		{
			name: "HappyPathTest",
			args: args{
				slashing: &ethpb.AttesterSlashing{
					Attestation_1: &ethpb.IndexedAttestation{
						AttestingIndices: []uint64{0, 1, 2},
						Data: &ethpb.AttestationData{
							BeaconBlockRoot: make([]byte, fieldparams.RootLength),
							Source: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
							Target: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
						},
						Signature: make([]byte, fieldparams.BLSSignatureLength),
					},
					Attestation_2: &ethpb.IndexedAttestation{
						AttestingIndices: []uint64{0, 1, 2},
						Data: &ethpb.AttestationData{
							BeaconBlockRoot: make([]byte, fieldparams.RootLength),
							Source: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
							Target: &ethpb.Checkpoint{
								Root: make([]byte, fieldparams.RootLength),
							},
						},
						Signature: make([]byte, fieldparams.BLSSignatureLength),
					},
				},
			},
			want: &types.AttesterSlashing{
				Attestation1: mock.IndexedAttestation(),
				Attestation2: mock.IndexedAttestation(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapAttesterSlashing(tt.args.slashing)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAttesterSlashing() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Attestation1, tt.want.Attestation1) {
				t.Errorf("MapAttesterSlashing() got = %v, want %v", got.Attestation1, tt.want.Attestation1)
			}
		})
	}
}

func TestMapBeaconBlockAltair(t *testing.T) {
	type args struct {
		block *ethpb.BeaconBlockAltair
	}
	tests := []struct {
		name    string
		args    args
		want    *types.BeaconBlockAltair
		wantErr bool
	}{
		{
			name: "Happy Path Test",
			args: args{
				block: &ethpb.BeaconBlockAltair{
					Slot:          0,
					ProposerIndex: 0,
					ParentRoot:    make([]byte, fieldparams.RootLength),
					StateRoot:     make([]byte, fieldparams.RootLength),
					Body: &ethpb.BeaconBlockBodyAltair{
						RandaoReveal: make([]byte, 32),
						Eth1Data: &ethpb.Eth1Data{
							DepositRoot:  make([]byte, fieldparams.RootLength),
							DepositCount: 0,
							BlockHash:    make([]byte, 32),
						},
						Graffiti: make([]byte, 32),
						ProposerSlashings: []*ethpb.ProposerSlashing{
							{
								Header_1: &ethpb.SignedBeaconBlockHeader{
									Header: &ethpb.BeaconBlockHeader{
										Slot:          0,
										ProposerIndex: 0,
										ParentRoot:    make([]byte, fieldparams.RootLength),
										StateRoot:     make([]byte, fieldparams.RootLength),
										BodyRoot:      make([]byte, fieldparams.RootLength),
									},
									Signature: make([]byte, fieldparams.BLSSignatureLength),
								},
								Header_2: &ethpb.SignedBeaconBlockHeader{
									Header: &ethpb.BeaconBlockHeader{
										Slot:          0,
										ProposerIndex: 0,
										ParentRoot:    make([]byte, fieldparams.RootLength),
										StateRoot:     make([]byte, fieldparams.RootLength),
										BodyRoot:      make([]byte, fieldparams.RootLength),
									},
									Signature: make([]byte, fieldparams.BLSSignatureLength),
								},
							},
						},
						AttesterSlashings: []*ethpb.AttesterSlashing{
							{
								Attestation_1: &ethpb.IndexedAttestation{
									AttestingIndices: []uint64{0, 1, 2},
									Data: &ethpb.AttestationData{
										BeaconBlockRoot: make([]byte, fieldparams.RootLength),
										Source: &ethpb.Checkpoint{
											Root: make([]byte, fieldparams.RootLength),
										},
										Target: &ethpb.Checkpoint{
											Root: make([]byte, fieldparams.RootLength),
										},
									},
									Signature: make([]byte, fieldparams.BLSSignatureLength),
								},
								Attestation_2: &ethpb.IndexedAttestation{
									AttestingIndices: []uint64{0, 1, 2},
									Data: &ethpb.AttestationData{
										BeaconBlockRoot: make([]byte, fieldparams.RootLength),
										Source: &ethpb.Checkpoint{
											Root: make([]byte, fieldparams.RootLength),
										},
										Target: &ethpb.Checkpoint{
											Root: make([]byte, fieldparams.RootLength),
										},
									},
									Signature: make([]byte, fieldparams.BLSSignatureLength),
								},
							},
						},
						Attestations: []*ethpb.Attestation{
							{
								AggregationBits: bitfield.Bitlist{0b1101},
								Data: &ethpb.AttestationData{
									BeaconBlockRoot: make([]byte, fieldparams.RootLength),
									Source: &ethpb.Checkpoint{
										Root: make([]byte, fieldparams.RootLength),
									},
									Target: &ethpb.Checkpoint{
										Root: make([]byte, fieldparams.RootLength),
									},
								},
								Signature: make([]byte, 96),
							},
						},
						Deposits: []*ethpb.Deposit{
							{
								Proof: [][]byte{[]byte("A")},
								Data: &ethpb.Deposit_Data{
									PublicKey:             make([]byte, fieldparams.BLSPubkeyLength),
									WithdrawalCredentials: make([]byte, 32),
									Amount:                0,
									Signature:             make([]byte, fieldparams.BLSSignatureLength),
								},
							},
						},
						VoluntaryExits: []*ethpb.SignedVoluntaryExit{
							{
								Exit: &ethpb.VoluntaryExit{
									Epoch:          0,
									ValidatorIndex: 0,
								},
								Signature: make([]byte, fieldparams.BLSSignatureLength),
							},
						},
						SyncAggregate: &ethpb.SyncAggregate{
							SyncCommitteeSignature: make([]byte, fieldparams.BLSSignatureLength),
							SyncCommitteeBits:      mock.SyncComitteeBits(),
						},
					},
				},
			},
			want:    mock.BeaconBlockAltair(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapBeaconBlockAltair(tt.args.block)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapBeaconBlockAltair() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Body, tt.want.Body) {
				t.Errorf("MapBeaconBlockAltair() got = %v, want %v", got.Body.SyncAggregate, tt.want.Body.SyncAggregate)
			}
		})
	}
}

func TestMapBeaconBlockBody(t *testing.T) {
	type args struct {
		body *ethpb.BeaconBlockBody
	}
	tests := []struct {
		name    string
		args    args
		want    *types.BeaconBlockBody
		wantErr bool
	}{
		{
			name: "Happy Path Test",
			args: args{
				body: &ethpb.BeaconBlockBody{
					RandaoReveal: make([]byte, 32),
					Eth1Data: &ethpb.Eth1Data{
						DepositRoot:  make([]byte, fieldparams.RootLength),
						DepositCount: 0,
						BlockHash:    make([]byte, 32),
					},
					Graffiti: make([]byte, 32),
					ProposerSlashings: []*ethpb.ProposerSlashing{
						{
							Header_1: &ethpb.SignedBeaconBlockHeader{
								Header: &ethpb.BeaconBlockHeader{
									Slot:          0,
									ProposerIndex: 0,
									ParentRoot:    make([]byte, fieldparams.RootLength),
									StateRoot:     make([]byte, fieldparams.RootLength),
									BodyRoot:      make([]byte, fieldparams.RootLength),
								},
								Signature: make([]byte, fieldparams.BLSSignatureLength),
							},
							Header_2: &ethpb.SignedBeaconBlockHeader{
								Header: &ethpb.BeaconBlockHeader{
									Slot:          0,
									ProposerIndex: 0,
									ParentRoot:    make([]byte, fieldparams.RootLength),
									StateRoot:     make([]byte, fieldparams.RootLength),
									BodyRoot:      make([]byte, fieldparams.RootLength),
								},
								Signature: make([]byte, fieldparams.BLSSignatureLength),
							},
						},
					},
					AttesterSlashings: []*ethpb.AttesterSlashing{
						{
							Attestation_1: &ethpb.IndexedAttestation{
								AttestingIndices: []uint64{0, 1, 2},
								Data: &ethpb.AttestationData{
									BeaconBlockRoot: make([]byte, fieldparams.RootLength),
									Source: &ethpb.Checkpoint{
										Root: make([]byte, fieldparams.RootLength),
									},
									Target: &ethpb.Checkpoint{
										Root: make([]byte, fieldparams.RootLength),
									},
								},
								Signature: make([]byte, fieldparams.BLSSignatureLength),
							},
							Attestation_2: &ethpb.IndexedAttestation{
								AttestingIndices: []uint64{0, 1, 2},
								Data: &ethpb.AttestationData{
									BeaconBlockRoot: make([]byte, fieldparams.RootLength),
									Source: &ethpb.Checkpoint{
										Root: make([]byte, fieldparams.RootLength),
									},
									Target: &ethpb.Checkpoint{
										Root: make([]byte, fieldparams.RootLength),
									},
								},
								Signature: make([]byte, fieldparams.BLSSignatureLength),
							},
						},
					},
					Attestations: []*ethpb.Attestation{
						{
							AggregationBits: bitfield.Bitlist{0b1101},
							Data: &ethpb.AttestationData{
								BeaconBlockRoot: make([]byte, fieldparams.RootLength),
								Source: &ethpb.Checkpoint{
									Root: make([]byte, fieldparams.RootLength),
								},
								Target: &ethpb.Checkpoint{
									Root: make([]byte, fieldparams.RootLength),
								},
							},
							Signature: make([]byte, 96),
						},
					},
					Deposits: []*ethpb.Deposit{
						{
							Proof: [][]byte{[]byte("A")},
							Data: &ethpb.Deposit_Data{
								PublicKey:             make([]byte, fieldparams.BLSPubkeyLength),
								WithdrawalCredentials: make([]byte, 32),
								Amount:                0,
								Signature:             make([]byte, fieldparams.BLSSignatureLength),
							},
						},
					},
					VoluntaryExits: []*ethpb.SignedVoluntaryExit{
						{
							Exit: &ethpb.VoluntaryExit{
								Epoch:          0,
								ValidatorIndex: 0,
							},
							Signature: make([]byte, fieldparams.BLSSignatureLength),
						},
					},
				},
			},
			want:    mock.BeaconBlockBody(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapBeaconBlockBody(tt.args.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapBeaconBlockBody() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapBeaconBlockBody() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapContributionAndProof(t *testing.T) {
	type args struct {
		contribution *ethpb.ContributionAndProof
	}
	tests := []struct {
		name    string
		args    args
		want    *types.ContributionAndProof
		wantErr bool
	}{
		{
			name: "Happy Path Test",
			args: args{
				contribution: &ethpb.ContributionAndProof{
					AggregatorIndex: 0,
					Contribution: &ethpb.SyncCommitteeContribution{
						Slot:              0,
						BlockRoot:         make([]byte, fieldparams.RootLength),
						SubcommitteeIndex: 0,
						AggregationBits:   mock.AggregationBits(),
						Signature:         make([]byte, fieldparams.BLSSignatureLength),
					},
					SelectionProof: make([]byte, fieldparams.BLSSignatureLength),
				},
			},
			want: mock.ContributionAndProof(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapContributionAndProof(tt.args.contribution)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapContributionAndProof() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapContributionAndProof() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapForkInfo(t *testing.T) {
	type args struct {
		slot                  primitives.Slot
		genesisValidatorsRoot []byte
	}

	tests := []struct {
		name    string
		args    args
		want    *types.ForkInfo
		wantErr bool
	}{
		{
			name: "Happy Path Test",
			args: args{
				slot:                  0,
				genesisValidatorsRoot: make([]byte, fieldparams.RootLength),
			},
			want:    mock.ForkInfo(),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapForkInfo(tt.args.slot, tt.args.genesisValidatorsRoot)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapForkInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapForkInfo() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapSyncAggregatorSelectionData(t *testing.T) {
	type args struct {
		data *ethpb.SyncAggregatorSelectionData
	}
	tests := []struct {
		name    string
		args    args
		want    *types.SyncAggregatorSelectionData
		wantErr bool
	}{
		{
			name: "Happy Path Test",
			args: args{
				data: &ethpb.SyncAggregatorSelectionData{
					Slot:              0,
					SubcommitteeIndex: 0,
				},
			},
			want: &types.SyncAggregatorSelectionData{
				Slot:              "0",
				SubcommitteeIndex: "0",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := types.MapSyncAggregatorSelectionData(tt.args.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapSyncAggregatorSelectionData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapSyncAggregatorSelectionData() got = %v, want %v", got, tt.want)
			}
		})
	}
}
