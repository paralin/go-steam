package steam

// PlayingSessionStateEvent reports whether this login may announce a game.
// Announcing playback while blocked can end the login with LoggedInElsewhere.
type PlayingSessionStateEvent struct {
	// PlayingBlocked indicates that another login holds the account's playing session.
	PlayingBlocked bool

	// PlayingApp identifies the game holding the playing session, or zero when idle.
	PlayingApp uint32
}
