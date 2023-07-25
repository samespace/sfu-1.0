package sfu

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrRoomNotFound             = errors.New("room not found")
	ErrRemoteRoomConnectTimeout = errors.New("timeout connecting to remote room")

	RoomTypeLocal  = "local"
	RoomTypeRemote = "remote"
)

// Manager is a struct that manages all the rooms
type Manager struct {
	rooms         map[string]*Room
	Context       context.Context
	CancelContext context.CancelFunc
	TurnServer    TurnServer
	UDPMux        *UDPMux
	Name          string
	mutex         sync.RWMutex
	Options       Options
	extension     []IManagerExtension
}

func NewManager(ctx context.Context, name string, options Options) *Manager {
	localCtx, cancel := context.WithCancel(ctx)

	turnServer := DefaultTurnServer()

	udpMux := NewUDPMux(ctx, options.WebRTCPort)

	m := &Manager{
		rooms:         make(map[string]*Room),
		Context:       localCtx,
		CancelContext: cancel,
		TurnServer:    turnServer,
		UDPMux:        udpMux,
		Name:          name,
		mutex:         sync.RWMutex{},
		Options:       options,
		extension:     make([]IManagerExtension, 0),
	}

	return m
}

func (m *Manager) AddExtension(extension IManagerExtension) {
	m.extension = append(m.extension, extension)
}

func (m *Manager) CreateRoomID() string {
	return GenerateID([]int{len(m.rooms)})
}

func (m *Manager) NewRoom(id, name, roomType string) *Room {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	newSFU := New(m.Context, m.TurnServer, m.UDPMux)

	room := newRoom(m.Context, id, name, newSFU, roomType)

	for _, ext := range m.extension {
		ext.OnNewRoom(m, room)
	}

	// TODO: what manager should do when a room is closed?
	// is there any neccesary resource to be released?
	room.OnRoomClosed(func(id string) {

	})

	room.OnClientRemoved(func(id string) {
		// TODO: should check if the room is empty and close it if it is
	})

	m.rooms[room.ID] = room

	return room
}

func (m *Manager) RoomsCount() int {
	return len(m.rooms)
}

func (m *Manager) GetRoom(id string) (*Room, error) {
	var (
		room *Room
		err  error
	)

	room, err = m.getRoom(id)
	if err == ErrRoomNotFound {
		for _, ext := range m.extension {
			room, err = ext.OnGetRoom(m, id)
		}

		return room, err
	}

	return room, nil
}

func (m *Manager) getRoom(id string) (*Room, error) {
	var (
		room *Room
		ok   bool
	)

	if room, ok = m.rooms[id]; !ok {
		return nil, ErrRoomNotFound
	}

	return room, nil
}

func (m *Manager) EndRoom(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var room *Room

	var ok bool

	if room, ok = m.rooms[id]; !ok {
		return ErrRoomNotFound
	}

	room.StopAllClients()

	return nil
}

func (m *Manager) Stop() {
	defer m.CancelContext()

	for _, room := range m.rooms {
		room.StopAllClients()
	}
}
