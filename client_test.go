package sfu

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/stretchr/testify/require"
)

func TestTracksSubscribe(t *testing.T) {
	t.Parallel()

	report := CheckRoutines(t)
	defer report()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create room manager first before create new room
	roomManager := NewManager(ctx, "test", Options{
		EnableMux:                true,
		EnableBandwidthEstimator: true,
		IceServers:               []webrtc.ICEServer{},
	})

	defer roomManager.Close()

	roomID := roomManager.CreateRoomID()
	roomName := "test-room"

	peerCount := 5

	// create new room
	roomOpts := DefaultRoomOptions()
	roomOpts.Codecs = []string{webrtc.MimeTypeH264, webrtc.MimeTypeOpus}
	testRoom, err := roomManager.NewRoom(roomID, roomName, RoomTypeLocal, roomOpts)

	defer testRoom.Close()

	require.NoError(t, err, "error creating room: %v", err)

	tracksAddedChan := make(chan int)
	tracksAvailableChan := make(chan int)
	trackChan := make(chan bool)

	peers := make([]*PC, 0)
	clients := make([]*Client, 0)

	for i := 0; i < peerCount; i++ {
		pc, client, _, _ := CreatePeerPair(ctx, TestLogger, testRoom, DefaultTestIceServers(), fmt.Sprintf("peer-%d", i), true, false)

		peers = append(peers, pc)
		clients = append(clients, client)

		client.OnTracksAdded(func(addedTracks []ITrack) {
			tracksAddedChan <- len(addedTracks)

			setTracks := make(map[string]TrackType)

			for _, track := range addedTracks {
				setTracks[track.ID()] = TrackTypeMedia
			}
			client.SetTracksSourceType(setTracks)
		})

		pc.PeerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			trackChan <- true
		})
	}

	timeout, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	tracksAdded := 0
	tracksAvailable := 0
	trackReceived := 0
	expectedTracks := (peerCount * 2) * (peerCount - 1)

Loop:
	for {
		select {
		case <-timeout.Done():
			break Loop
		case added := <-tracksAddedChan:
			tracksAdded += added
		case available := <-tracksAvailableChan:
			tracksAvailable += available
		case <-trackChan:
			trackReceived++
			if trackReceived == expectedTracks {
				break Loop
			}
		}

	}

	require.Equal(t, peerCount*2, tracksAdded)
	require.Equal(t, expectedTracks, trackReceived)

	for _, client := range clients {
		require.NoError(t, testRoom.StopClient(client.id))
	}

	for _, pc := range peers {
		require.NoError(t, pc.PeerConnection.Close())
	}

}

// TODO: this is can't be work without a new SimulcastLocalTrack that can add header extension to the packet

func TestSimulcastTrack(t *testing.T) {
	t.Parallel()

	report := CheckRoutines(t)
	defer report()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create room manager first before create new room
	roomManager := NewManager(ctx, "test", Options{
		EnableMux:                true,
		EnableBandwidthEstimator: true,
		IceServers:               []webrtc.ICEServer{},
	})

	defer roomManager.Close()

	roomID := roomManager.CreateRoomID()
	roomName := "test-room"

	// create new room
	roomOpts := DefaultRoomOptions()
	roomOpts.Codecs = []string{webrtc.MimeTypeH264, webrtc.MimeTypeOpus}
	testRoom, err := roomManager.NewRoom(roomID, roomName, RoomTypeLocal, roomOpts)
	require.NoError(t, err, "error creating room: %v", err)

	client1, pc1 := addSimulcastPair(t, ctx, testRoom, "peer1")
	client2, pc2 := addSimulcastPair(t, ctx, testRoom, "peer2")

	defer func() {
		_ = testRoom.StopClient(client1.id)
		_ = testRoom.StopClient(client2.id)
	}()

	trackChan := make(chan *webrtc.TrackRemote)

	pc1.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		trackChan <- track
	})

	pc2.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		trackChan <- track
	})

	// wait for track added
	timeout, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	trackCount := 0
Loop:
	for {
		select {
		case <-timeout.Done():
			t.Fatal("timeout waiting for track added")
			break Loop
		case <-trackChan:
			trackCount++
			t.Log("track added ", trackCount)
			if trackCount == 2 {
				break Loop
			}
		}
	}
}

func addSimulcastPair(t *testing.T, ctx context.Context, room *Room, peerName string) (*Client, *webrtc.PeerConnection) {
	pc, client, _, _ := CreatePeerPair(ctx, TestLogger, room, DefaultTestIceServers(), peerName, true, true)
	client.OnTracksAvailable(func(availableTracks []ITrack) {

		tracksReq := make([]SubscribeTrackRequest, 0)
		for _, track := range availableTracks {
			tracksReq = append(tracksReq, SubscribeTrackRequest{
				ClientID: track.ClientID(),
				TrackID:  track.ID(),
			})
		}
		err := client.SubscribeTracks(tracksReq)
		require.NoError(t, err)
	})

	client.OnTracksAdded(func(addedTracks []ITrack) {
		setTracks := make(map[string]TrackType, 0)
		for _, track := range addedTracks {
			setTracks[track.ID()] = TrackTypeMedia
		}
		client.SetTracksSourceType(setTracks)
	})

	pc.PeerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		t.Log("test: on track", track.Msid())
	})

	return client, pc.PeerConnection
}

func TestClientDataChannel(t *testing.T) {
	t.Parallel()

	report := CheckRoutines(t)
	defer report()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create room manager first before create new room
	roomManager := NewManager(ctx, "test", Options{
		EnableMux:                true,
		EnableBandwidthEstimator: true,
		IceServers:               []webrtc.ICEServer{},
	})

	defer roomManager.Close()

	roomID := roomManager.CreateRoomID()
	roomName := "test-room"

	// create new room
	roomOpts := DefaultRoomOptions()
	roomOpts.Codecs = []string{webrtc.MimeTypeH264, webrtc.MimeTypeOpus}
	testRoom, err := roomManager.NewRoom(roomID, roomName, RoomTypeLocal, roomOpts)
	require.NoError(t, err, "error creating room: %v", err)

	dcChan := make(chan *webrtc.DataChannel)
	pc, client, _, connChan := CreateDataPair(ctx, TestLogger, testRoom, roomManager.options.IceServers, "peer1", func(c *webrtc.DataChannel) {
		dcChan <- c
	})

	timeout, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)

	defer cancelTimeout()

	select {
	case <-timeout.Done():
		t.Fatal("timeout waiting for data channel")
	case state := <-connChan:
		if state == webrtc.PeerConnectionStateConnected {
			_, _ = pc.CreateDataChannel("test", nil)

			negotiate(pc, client, TestLogger)
		}
	case dc := <-dcChan:
		require.Equal(t, "internal", dc.Label())
	}
}
