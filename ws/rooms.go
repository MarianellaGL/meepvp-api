package ws

// Room name prefixes. A client auto-joins its own user room on connect. Rooms
// under RoomPrefixAdmin can only be joined by clients whose token carries the
// admin role; RoomPrefixAdminSpy + clientID is where admin observers receive
// a copy of that client's traffic.
const (
	RoomPrefixUser     = "user:"
	RoomPrefixAdmin    = "admin:"
	RoomPrefixAdminSpy = "admin:spy:"
)
