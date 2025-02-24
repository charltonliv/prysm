package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prysmaticlabs/prysm/v5/api"
	"github.com/prysmaticlabs/prysm/v5/api/server/structs"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/core/feed"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/core/feed/operation"
	statefeed "github.com/prysmaticlabs/prysm/v5/beacon-chain/core/feed/state"
	"github.com/prysmaticlabs/prysm/v5/beacon-chain/core/helpers"
	chaintime "github.com/prysmaticlabs/prysm/v5/beacon-chain/core/time"
	"github.com/prysmaticlabs/prysm/v5/config/params"
	"github.com/prysmaticlabs/prysm/v5/consensus-types/interfaces"
	payloadattribute "github.com/prysmaticlabs/prysm/v5/consensus-types/payload-attribute"
	"github.com/prysmaticlabs/prysm/v5/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v5/monitoring/tracing/trace"
	"github.com/prysmaticlabs/prysm/v5/network/httputil"
	engine "github.com/prysmaticlabs/prysm/v5/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/v5/proto/eth/v1"
	eth "github.com/prysmaticlabs/prysm/v5/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/v5/runtime/version"
	"github.com/prysmaticlabs/prysm/v5/time/slots"
)

const DefaultEventFeedDepth = 1000
const payloadAttributeTimeout = 2 * time.Second

const (
	InvalidTopic = "__invalid__"
	// HeadTopic represents a new chain head event topic.
	HeadTopic = "head"
	// BlockTopic represents a new produced block event topic.
	BlockTopic = "block"
	// AttestationTopic represents a new submitted attestation event topic.
	AttestationTopic = "attestation"
	// VoluntaryExitTopic represents a new performed voluntary exit event topic.
	VoluntaryExitTopic = "voluntary_exit"
	// FinalizedCheckpointTopic represents a new finalized checkpoint event topic.
	FinalizedCheckpointTopic = "finalized_checkpoint"
	// ChainReorgTopic represents a chain reorganization event topic.
	ChainReorgTopic = "chain_reorg"
	// SyncCommitteeContributionTopic represents a new sync committee contribution event topic.
	SyncCommitteeContributionTopic = "contribution_and_proof"
	// BLSToExecutionChangeTopic represents a new received BLS to execution change event topic.
	BLSToExecutionChangeTopic = "bls_to_execution_change"
	// PayloadAttributesTopic represents a new payload attributes for execution payload building event topic.
	PayloadAttributesTopic = "payload_attributes"
	// BlobSidecarTopic represents a new blob sidecar event topic
	BlobSidecarTopic = "blob_sidecar"
	// ProposerSlashingTopic represents a new proposer slashing event topic
	ProposerSlashingTopic = "proposer_slashing"
	// AttesterSlashingTopic represents a new attester slashing event topic
	AttesterSlashingTopic = "attester_slashing"
	// LightClientFinalityUpdateTopic represents a new light client finality update event topic.
	LightClientFinalityUpdateTopic = "light_client_finality_update"
	// LightClientOptimisticUpdateTopic represents a new light client optimistic update event topic.
	LightClientOptimisticUpdateTopic = "light_client_optimistic_update"
)

var (
	errInvalidTopicName   = errors.New("invalid topic name")
	errNoValidTopics      = errors.New("no valid topics specified")
	errSlowReader         = errors.New("client failed to read fast enough to keep outgoing buffer below threshold")
	errNotRequested       = errors.New("event not requested by client")
	errUnhandledEventData = errors.New("unable to represent event data in the event stream")
	errWriterUnusable     = errors.New("http response writer is unusable")
)

var httpSSEErrorCount = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_sse_error_count",
		Help: "Total HTTP errors for server sent events endpoint",
	},
	[]string{"endpoint", "error"},
)

// The eventStreamer uses lazyReaders to defer serialization until the moment the value is ready to be written to the client.
type lazyReader func() io.Reader

var opsFeedEventTopics = map[feed.EventType]string{
	operation.AggregatedAttReceived:             AttestationTopic,
	operation.UnaggregatedAttReceived:           AttestationTopic,
	operation.ExitReceived:                      VoluntaryExitTopic,
	operation.SyncCommitteeContributionReceived: SyncCommitteeContributionTopic,
	operation.BLSToExecutionChangeReceived:      BLSToExecutionChangeTopic,
	operation.BlobSidecarReceived:               BlobSidecarTopic,
	operation.AttesterSlashingReceived:          AttesterSlashingTopic,
	operation.ProposerSlashingReceived:          ProposerSlashingTopic,
}

var stateFeedEventTopics = map[feed.EventType]string{
	statefeed.NewHead:                     HeadTopic,
	statefeed.FinalizedCheckpoint:         FinalizedCheckpointTopic,
	statefeed.LightClientFinalityUpdate:   LightClientFinalityUpdateTopic,
	statefeed.LightClientOptimisticUpdate: LightClientOptimisticUpdateTopic,
	statefeed.Reorg:                       ChainReorgTopic,
	statefeed.BlockProcessed:              BlockTopic,
	statefeed.PayloadAttributes:           PayloadAttributesTopic,
}

var topicsForStateFeed = topicsForFeed(stateFeedEventTopics)
var topicsForOpsFeed = topicsForFeed(opsFeedEventTopics)

func topicsForFeed(em map[feed.EventType]string) map[string]bool {
	topics := make(map[string]bool, len(em))
	for _, topic := range em {
		topics[topic] = true
	}
	return topics
}

type topicRequest struct {
	topics        map[string]bool
	needStateFeed bool
	needOpsFeed   bool
}

func (req *topicRequest) requested(topic string) bool {
	return req.topics[topic]
}

func newTopicRequest(topics []string) (*topicRequest, error) {
	req := &topicRequest{topics: make(map[string]bool)}
	for _, name := range topics {
		if topicsForStateFeed[name] {
			req.needStateFeed = true
		} else if topicsForOpsFeed[name] {
			req.needOpsFeed = true
		} else {
			return nil, errors.Wrapf(errInvalidTopicName, name)
		}
		req.topics[name] = true
	}
	if len(req.topics) == 0 || (!req.needStateFeed && !req.needOpsFeed) {
		return nil, errNoValidTopics
	}

	return req, nil
}

// StreamEvents provides an endpoint to subscribe to the beacon node Server-Sent-Events stream.
// Consumers should use the eventsource implementation to listen for those events.
// Servers may send SSE comments beginning with ':' for any purpose,
// including to keep the event stream connection alive in the presence of proxy servers.
func (s *Server) StreamEvents(w http.ResponseWriter, r *http.Request) {
	var err error
	defer func() {
		if err != nil {
			httpSSEErrorCount.WithLabelValues(r.URL.Path, err.Error()).Inc()
		}
	}()

	log.Debug("Starting StreamEvents handler")
	ctx, span := trace.StartSpan(r.Context(), "events.StreamEvents")
	defer span.End()

	topics, err := newTopicRequest(r.URL.Query()["topics"])
	if err != nil {
		httputil.HandleError(w, err.Error(), http.StatusBadRequest)
		return
	}

	timeout := s.EventWriteTimeout
	if timeout == 0 {
		timeout = time.Duration(params.BeaconConfig().SecondsPerSlot) * time.Second
	}
	ka := s.KeepAliveInterval
	if ka == 0 {
		ka = timeout
	}
	buffSize := s.EventFeedDepth
	if buffSize == 0 {
		buffSize = DefaultEventFeedDepth
	}

	api.SetSSEHeaders(w)
	sw := newStreamingResponseController(w, timeout)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	es := newEventStreamer(buffSize, ka)

	go es.outboxWriteLoop(ctx, cancel, sw, r.URL.Path)
	if err := es.recvEventLoop(ctx, cancel, topics, s); err != nil {
		log.WithError(err).Debug("Shutting down StreamEvents handler.")
	}
	cleanupStart := time.Now()
	es.waitForExit()
	log.WithField("cleanup_wait", time.Since(cleanupStart)).Debug("streamEvents shutdown complete")
}

func newEventStreamer(buffSize int, ka time.Duration) *eventStreamer {
	return &eventStreamer{
		outbox:        make(chan lazyReader, buffSize),
		keepAlive:     ka,
		openUntilExit: make(chan struct{}),
	}
}

type eventStreamer struct {
	outbox        chan lazyReader
	keepAlive     time.Duration
	openUntilExit chan struct{}
}

func (es *eventStreamer) recvEventLoop(ctx context.Context, cancel context.CancelFunc, req *topicRequest, s *Server) error {
	defer close(es.outbox)
	defer cancel()
	eventsChan := make(chan *feed.Event, len(es.outbox))
	if req.needOpsFeed {
		opsSub := s.OperationNotifier.OperationFeed().Subscribe(eventsChan)
		defer opsSub.Unsubscribe()
	}
	if req.needStateFeed {
		stateSub := s.StateNotifier.StateFeed().Subscribe(eventsChan)
		defer stateSub.Unsubscribe()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-eventsChan:
			lr, err := s.lazyReaderForEvent(ctx, event, req)
			if err != nil {
				if !errors.Is(err, errNotRequested) {
					log.WithField("event_type", fmt.Sprintf("%v", event.Data)).WithError(err).Error("StreamEvents API endpoint received an event it was unable to handle.")
				}
				continue
			}
			// If the client can't keep up, the outbox will eventually completely fill, at which
			// safeWrite will error, and we'll hit the below return statement, at which point the deferred
			// Unsuscribe calls will be made and the event feed will stop writing to this channel.
			// Since the outbox and event stream channels are separately buffered, the event subscription
			// channel should stay relatively empty, which gives this loop time to unsubscribe
			// and cleanup before the event stream channel fills and disrupts other readers.
			if err := es.safeWrite(ctx, lr); err != nil {
				// note: we could hijack the connection and close it here. Does that cause issues? What are the benefits?
				// A benefit of hijack and close is that it may force an error on the remote end, however just closing the context of the
				// http handler may be sufficient to cause the remote http response reader to close.
				if errors.Is(err, errSlowReader) {
					log.WithError(err).Warn("Client is unable to keep up with event stream, shutting down.")
				}
				return err
			}
		}
	}
}

func (es *eventStreamer) safeWrite(ctx context.Context, rf func() io.Reader) error {
	if rf == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case es.outbox <- rf:
		return nil
	default:
		// If this is the case, the select case to write to the outbox could not proceed, meaning the outbox is full.
		// If a reader can't keep up with the stream, we shut them down.
		return errSlowReader
	}
}

// newlineReader is used to write keep-alives to the client.
// keep-alives in the sse protocol are a single ':' colon followed by 2 newlines.
func newlineReader() io.Reader {
	return bytes.NewBufferString(":\n\n")
}

// outboxWriteLoop runs in a separate goroutine. Its job is to write the values in the outbox to
// the client as fast as the client can read them.
func (es *eventStreamer) outboxWriteLoop(ctx context.Context, cancel context.CancelFunc, w *streamingResponseWriterController, endpoint string) {
	var err error
	defer func() {
		if err != nil {
			log.WithError(err).Debug("Event streamer shutting down due to error.")
			httpSSEErrorCount.WithLabelValues(endpoint, err.Error()).Inc()
		}
		es.exit()
	}()
	defer cancel()
	// Write a keepalive at the start to test the connection and simplify test setup.
	if err = es.writeOutbox(ctx, w, nil); err != nil {
		return
	}

	kaT := time.NewTimer(es.keepAlive)
	// Ensure the keepalive timer is stopped and drained if it has fired.
	defer func() {
		if !kaT.Stop() {
			<-kaT.C
		}
	}()
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-kaT.C:
			if err = es.writeOutbox(ctx, w, nil); err != nil {
				return
			}
			// In this case the timer doesn't need to be Stopped before the Reset call after the select statement,
			// because the timer has already fired.
		case lr := <-es.outbox:
			if err = es.writeOutbox(ctx, w, lr); err != nil {
				return
			}
			// We don't know if the timer fired concurrently to this case being ready, so we need to check the return
			// of Stop and drain the timer channel if it fired. We won't need to do this in go 1.23.
			if !kaT.Stop() {
				<-kaT.C
			}
		}
		kaT.Reset(es.keepAlive)
	}
}

func (es *eventStreamer) exit() {
	drained := 0
	for range es.outbox {
		drained += 1
	}
	log.WithField("undelivered_events", drained).Debug("Event stream outbox drained.")
	close(es.openUntilExit)
}

// waitForExit blocks until the outboxWriteLoop has exited.
// While this function blocks, it is not yet safe to exit the http handler,
// because the outboxWriteLoop may still be writing to the http ResponseWriter.
func (es *eventStreamer) waitForExit() {
	<-es.openUntilExit
}

func writeLazyReaderWithRecover(w *streamingResponseWriterController, lr lazyReader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.WithField("panic", r).Error("Recovered from panic while writing event to client.")
			err = errWriterUnusable
		}
	}()
	r := lr()
	out, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

func (es *eventStreamer) writeOutbox(ctx context.Context, w *streamingResponseWriterController, first lazyReader) error {
	// The outboxWriteLoop is responsible for managing the keep-alive timer and toggling between reading from the outbox
	// when it is ready, only allowing the keep-alive to fire when there hasn't been a write in the keep-alive interval.
	// Since outboxWriteLoop will get either the first event or the keep-alive, we let it pass in the first event to write,
	// either the event's lazyReader, or nil for a keep-alive.
	needKeepAlive := true
	if first != nil {
		if err := writeLazyReaderWithRecover(w, first); err != nil {
			return err
		}
		needKeepAlive = false
	}
	// While the first event was being read by the client, further events may be queued in the outbox.
	// We can drain them right away rather than go back out to the outer select statement, where the keepAlive timer
	// may have fired, triggering an unnecessary extra keep-alive write and flush.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case rf := <-es.outbox:
			// We don't want to call Flush until we've exhausted all the writes - it's always preferable to
			// just keep draining the outbox and rely on the underlying Write code to flush+block when it
			// needs to based on buffering. Whenever we fill the buffer with a string of writes, the underlying
			// code will flush on its own, so it's better to explicitly flush only once, after we've totally
			// drained the outbox, to catch any dangling bytes stuck in a buffer.
			if err := writeLazyReaderWithRecover(w, rf); err != nil {
				return err
			}
			needKeepAlive = false
		default:
			if needKeepAlive {
				if err := writeLazyReaderWithRecover(w, newlineReader); err != nil {
					return err
				}
			}
			return w.Flush()
		}
	}
}

func jsonMarshalReader(name string, v any) io.Reader {
	d, err := json.Marshal(v)
	if err != nil {
		log.WithError(err).WithField("type_name", fmt.Sprintf("%T", v)).Error("Could not marshal event data.")
		return nil
	}
	return bytes.NewBufferString("event: " + name + "\ndata: " + string(d) + "\n\n")
}

func topicForEvent(event *feed.Event) string {
	switch event.Data.(type) {
	case *operation.AggregatedAttReceivedData:
		return AttestationTopic
	case *operation.UnAggregatedAttReceivedData:
		return AttestationTopic
	case *operation.ExitReceivedData:
		return VoluntaryExitTopic
	case *operation.SyncCommitteeContributionReceivedData:
		return SyncCommitteeContributionTopic
	case *operation.BLSToExecutionChangeReceivedData:
		return BLSToExecutionChangeTopic
	case *operation.BlobSidecarReceivedData:
		return BlobSidecarTopic
	case *operation.AttesterSlashingReceivedData:
		return AttesterSlashingTopic
	case *operation.ProposerSlashingReceivedData:
		return ProposerSlashingTopic
	case *ethpb.EventHead:
		return HeadTopic
	case *ethpb.EventFinalizedCheckpoint:
		return FinalizedCheckpointTopic
	case interfaces.LightClientFinalityUpdate:
		return LightClientFinalityUpdateTopic
	case interfaces.LightClientOptimisticUpdate:
		return LightClientOptimisticUpdateTopic
	case *ethpb.EventChainReorg:
		return ChainReorgTopic
	case *statefeed.BlockProcessedData:
		return BlockTopic
	case payloadattribute.EventData:
		return PayloadAttributesTopic
	default:
		return InvalidTopic
	}
}

func (s *Server) lazyReaderForEvent(ctx context.Context, event *feed.Event, topics *topicRequest) (lazyReader, error) {
	eventName := topicForEvent(event)
	if !topics.requested(eventName) {
		return nil, errNotRequested
	}
	if event == nil || event.Data == nil {
		return nil, errors.New("event or event data is nil")
	}
	switch v := event.Data.(type) {
	case payloadattribute.EventData:
		return s.payloadAttributesReader(ctx, v)
	case *ethpb.EventHead:
		// The head event is a special case because, if the client requested the payload attributes topic,
		// we send two event messages in reaction; the head event and the payload attributes.
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.HeadEventFromV1(v))
		}, nil
	case *operation.AggregatedAttReceivedData:
		return func() io.Reader {
			att := structs.AttFromConsensus(v.Attestation.Aggregate)
			return jsonMarshalReader(eventName, att)
		}, nil
	case *operation.UnAggregatedAttReceivedData:
		switch att := v.Attestation.(type) {
		case *eth.Attestation:
			return func() io.Reader {
				att := structs.AttFromConsensus(att)
				return jsonMarshalReader(eventName, att)
			}, nil
		case *eth.AttestationElectra:
			return func() io.Reader {
				att := structs.AttElectraFromConsensus(att)
				return jsonMarshalReader(eventName, att)
			}, nil
		default:
			return nil, errors.Wrapf(errUnhandledEventData, "Unexpected type %T for the .Attestation field of UnAggregatedAttReceivedData", v.Attestation)
		}
	case *operation.ExitReceivedData:
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.SignedExitFromConsensus(v.Exit))
		}, nil
	case *operation.SyncCommitteeContributionReceivedData:
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.SignedContributionAndProofFromConsensus(v.Contribution))
		}, nil
	case *operation.BLSToExecutionChangeReceivedData:
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.SignedBLSChangeFromConsensus(v.Change))
		}, nil
	case *operation.BlobSidecarReceivedData:
		return func() io.Reader {
			versionedHash := primitives.ConvertKzgCommitmentToVersionedHash(v.Blob.KzgCommitment)
			return jsonMarshalReader(eventName, &structs.BlobSidecarEvent{
				BlockRoot:     hexutil.Encode(v.Blob.BlockRootSlice()),
				Index:         fmt.Sprintf("%d", v.Blob.Index),
				Slot:          fmt.Sprintf("%d", v.Blob.Slot()),
				VersionedHash: versionedHash.String(),
				KzgCommitment: hexutil.Encode(v.Blob.KzgCommitment),
			})
		}, nil
	case *operation.AttesterSlashingReceivedData:
		switch slashing := v.AttesterSlashing.(type) {
		case *eth.AttesterSlashing:
			return func() io.Reader {
				return jsonMarshalReader(eventName, structs.AttesterSlashingFromConsensus(slashing))
			}, nil
		case *eth.AttesterSlashingElectra:
			return func() io.Reader {
				return jsonMarshalReader(eventName, structs.AttesterSlashingElectraFromConsensus(slashing))
			}, nil
		default:
			return nil, errors.Wrapf(errUnhandledEventData, "Unexpected type %T for the .AttesterSlashing field of AttesterSlashingReceivedData", v.AttesterSlashing)
		}
	case *operation.ProposerSlashingReceivedData:
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.ProposerSlashingFromConsensus(v.ProposerSlashing))
		}, nil
	case *ethpb.EventFinalizedCheckpoint:
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.FinalizedCheckpointEventFromV1(v))
		}, nil
	case interfaces.LightClientFinalityUpdate:
		cv, err := structs.LightClientFinalityUpdateFromConsensus(v)
		if err != nil {
			return nil, errors.Wrap(err, "LightClientFinalityUpdate conversion failure")
		}
		ev := &structs.LightClientFinalityUpdateEvent{
			Version: version.String(v.Version()),
			Data:    cv,
		}
		return func() io.Reader {
			return jsonMarshalReader(eventName, ev)
		}, nil
	case interfaces.LightClientOptimisticUpdate:
		cv, err := structs.LightClientOptimisticUpdateFromConsensus(v)
		if err != nil {
			return nil, errors.Wrap(err, "LightClientOptimisticUpdate conversion failure")
		}
		ev := &structs.LightClientOptimisticUpdateEvent{
			Version: version.String(v.Version()),
			Data:    cv,
		}
		return func() io.Reader {
			return jsonMarshalReader(eventName, ev)
		}, nil
	case *ethpb.EventChainReorg:
		return func() io.Reader {
			return jsonMarshalReader(eventName, structs.EventChainReorgFromV1(v))
		}, nil
	case *statefeed.BlockProcessedData:
		blockRoot, err := v.SignedBlock.Block().HashTreeRoot()
		if err != nil {
			return nil, errors.Wrap(err, "could not compute block root for BlockProcessedData state feed event")
		}
		return func() io.Reader {
			blk := &structs.BlockEvent{
				Slot:                fmt.Sprintf("%d", v.Slot),
				Block:               hexutil.Encode(blockRoot[:]),
				ExecutionOptimistic: v.Optimistic,
			}
			return jsonMarshalReader(eventName, blk)
		}, nil
	default:
		return nil, errors.Wrapf(errUnhandledEventData, "event data type %T unsupported", v)
	}
}

var errUnsupportedPayloadAttribute = errors.New("cannot compute payload attributes pre-Bellatrix")

func (s *Server) computePayloadAttributes(ctx context.Context, ev payloadattribute.EventData) (payloadattribute.Attributer, error) {
	v := ev.HeadState.Version()
	if v < version.Bellatrix {
		return nil, errors.Wrapf(errUnsupportedPayloadAttribute, "%s is not supported", version.String(v))
	}

	t, err := slots.ToTime(ev.HeadState.GenesisTime(), ev.HeadState.Slot())
	if err != nil {
		return nil, errors.Wrap(err, "could not get head state slot time")
	}
	timestamp := uint64(t.Unix())
	prevRando, err := helpers.RandaoMix(ev.HeadState, chaintime.CurrentEpoch(ev.HeadState))
	if err != nil {
		return nil, errors.Wrap(err, "could not get head state randao mix")
	}
	proposerIndex, err := helpers.BeaconProposerIndex(ctx, ev.HeadState)
	if err != nil {
		return nil, errors.Wrap(err, "could not get head state proposer index")
	}
	feeRecpt := params.BeaconConfig().DefaultFeeRecipient.Bytes()
	tValidator, exists := s.TrackedValidatorsCache.Validator(proposerIndex)
	if exists {
		feeRecpt = tValidator.FeeRecipient[:]
	}

	if v == version.Bellatrix {
		return payloadattribute.New(&engine.PayloadAttributes{
			Timestamp:             timestamp,
			PrevRandao:            prevRando,
			SuggestedFeeRecipient: feeRecpt,
		})
	}

	w, _, err := ev.HeadState.ExpectedWithdrawals()
	if err != nil {
		return nil, errors.Wrap(err, "could not get withdrawals from head state")
	}
	if v == version.Capella {
		return payloadattribute.New(&engine.PayloadAttributesV2{
			Timestamp:             timestamp,
			PrevRandao:            prevRando,
			SuggestedFeeRecipient: feeRecpt,
			Withdrawals:           w,
		})
	}

	pr, err := ev.HeadBlock.Block().HashTreeRoot()
	if err != nil {
		return nil, errors.Wrap(err, "could not compute head block root")
	}
	return payloadattribute.New(&engine.PayloadAttributesV3{
		Timestamp:             timestamp,
		PrevRandao:            prevRando,
		SuggestedFeeRecipient: feeRecpt,
		Withdrawals:           w,
		ParentBeaconBlockRoot: pr[:],
	})
}

type asyncPayloadAttrData struct {
	data    json.RawMessage
	version string
	err     error
}

func (s *Server) fillEventData(ctx context.Context, ev payloadattribute.EventData) (payloadattribute.EventData, error) {
	if ev.HeadBlock == nil || ev.HeadBlock.IsNil() {
		hb, err := s.HeadFetcher.HeadBlock(ctx)
		if err != nil {
			return ev, errors.Wrap(err, "Could not look up head block")
		}
		root, err := hb.Block().HashTreeRoot()
		if err != nil {
			return ev, errors.Wrap(err, "Could not compute head block root")
		}
		if ev.HeadRoot != root {
			return ev, errors.Wrap(err, "head root changed before payload attribute event handler execution")
		}
		ev.HeadBlock = hb
		payload, err := hb.Block().Body().Execution()
		if err != nil {
			return ev, errors.Wrap(err, "Could not get execution payload for head block")
		}
		ev.ParentBlockHash = payload.BlockHash()
		ev.ParentBlockNumber = payload.BlockNumber()
	}

	attr := ev.Attributer
	if attr == nil || attr.IsEmpty() {
		attr, err := s.computePayloadAttributes(ctx, ev)
		if err != nil {
			return ev, errors.Wrap(err, "Could not compute payload attributes")
		}
		ev.Attributer = attr
	}
	return ev, nil
}

// This event stream is intended to be used by builders and relays.
// Parent fields are based on state at N_{current_slot}, while the rest of fields are based on state of N_{current_slot + 1}
func (s *Server) payloadAttributesReader(ctx context.Context, ev payloadattribute.EventData) (lazyReader, error) {
	ctx, cancel := context.WithTimeout(ctx, payloadAttributeTimeout)
	edc := make(chan asyncPayloadAttrData)
	go func() {
		d := asyncPayloadAttrData{
			version: version.String(ev.HeadState.Version()),
		}

		defer func() {
			edc <- d
		}()
		ev, err := s.fillEventData(ctx, ev)
		if err != nil {
			d.err = errors.Wrap(err, "Could not fill event data")
			return
		}
		attributesBytes, err := marshalAttributes(ev.Attributer)
		if err != nil {
			d.err = errors.Wrap(err, "errors marshaling payload attributes to json")
			return
		}
		d.data, d.err = json.Marshal(structs.PayloadAttributesEventData{
			ProposerIndex:     strconv.FormatUint(uint64(ev.ProposerIndex), 10),
			ProposalSlot:      strconv.FormatUint(uint64(ev.ProposalSlot), 10),
			ParentBlockNumber: strconv.FormatUint(ev.ParentBlockNumber, 10),
			ParentBlockRoot:   hexutil.Encode(ev.ParentBlockRoot),
			ParentBlockHash:   hexutil.Encode(ev.ParentBlockHash),
			PayloadAttributes: attributesBytes,
		})
		if d.err != nil {
			d.err = errors.Wrap(d.err, "errors marshaling payload attributes event data to json")
		}
	}()
	return func() io.Reader {
		defer cancel()
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Warn("Context canceled while waiting for payload attributes event data")
			return nil
		case ed := <-edc:
			if ed.err != nil {
				log.WithError(ed.err).Warn("Error while marshaling payload attributes event data")
				return nil
			}
			return jsonMarshalReader(PayloadAttributesTopic, &structs.PayloadAttributesEvent{
				Version: ed.version,
				Data:    ed.data,
			})
		}
	}, nil
}

func marshalAttributes(attr payloadattribute.Attributer) ([]byte, error) {
	v := attr.Version()
	if v < version.Bellatrix {
		return nil, errors.Wrapf(errUnsupportedPayloadAttribute, "Payload version %s is not supported", version.String(v))
	}

	timestamp := strconv.FormatUint(attr.Timestamp(), 10)
	prevRandao := hexutil.Encode(attr.PrevRandao())
	feeRecpt := hexutil.Encode(attr.SuggestedFeeRecipient())
	if v == version.Bellatrix {
		return json.Marshal(&structs.PayloadAttributesV1{
			Timestamp:             timestamp,
			PrevRandao:            prevRandao,
			SuggestedFeeRecipient: feeRecpt,
		})
	}
	w, err := attr.Withdrawals()
	if err != nil {
		return nil, errors.Wrap(err, "could not get withdrawals from payload attributes event")
	}
	withdrawals := structs.WithdrawalsFromConsensus(w)
	if v == version.Capella {
		return json.Marshal(&structs.PayloadAttributesV2{
			Timestamp:             timestamp,
			PrevRandao:            prevRandao,
			SuggestedFeeRecipient: feeRecpt,
			Withdrawals:           withdrawals,
		})
	}
	parentRoot, err := attr.ParentBeaconBlockRoot()
	if err != nil {
		return nil, errors.Wrap(err, "could not get parent beacon block root from payload attributes event")
	}
	return json.Marshal(&structs.PayloadAttributesV3{
		Timestamp:             timestamp,
		PrevRandao:            prevRandao,
		SuggestedFeeRecipient: feeRecpt,
		Withdrawals:           withdrawals,
		ParentBeaconBlockRoot: hexutil.Encode(parentRoot),
	})
}

func newStreamingResponseController(rw http.ResponseWriter, timeout time.Duration) *streamingResponseWriterController {
	rc := http.NewResponseController(rw)
	return &streamingResponseWriterController{
		timeout: timeout,
		rw:      rw,
		rc:      rc,
	}
}

// streamingResponseWriterController provides an interface similar to an http.ResponseWriter,
// wrapping an http.ResponseWriter and an http.ResponseController, using the ResponseController
// to set and clear deadlines for Write and Flush methods, and delegating to the underlying
// types to Write and Flush.
type streamingResponseWriterController struct {
	timeout time.Duration
	rw      http.ResponseWriter
	rc      *http.ResponseController
}

func (c *streamingResponseWriterController) Write(b []byte) (int, error) {
	if err := c.setDeadline(); err != nil {
		return 0, err
	}
	out, err := c.rw.Write(b)
	if err != nil {
		return out, err
	}
	return out, c.clearDeadline()
}

func (c *streamingResponseWriterController) setDeadline() error {
	return c.rc.SetWriteDeadline(time.Now().Add(c.timeout))
}

func (c *streamingResponseWriterController) clearDeadline() error {
	return c.rc.SetWriteDeadline(time.Time{})
}

func (c *streamingResponseWriterController) Flush() error {
	if err := c.setDeadline(); err != nil {
		return err
	}
	if err := c.rc.Flush(); err != nil {
		return err
	}
	return c.clearDeadline()
}
