// common/logger/events.go

package logger

import (
	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// Stable event ids. The id is what a log reader greps for and what the locale
// catalog translates, so it outlives whatever the message becomes in either
// language.
const (
	EventConfigLoaded   s99logger.MessageID = messages.KeyLogsConfigLoaded
	EventMigrationsDone s99logger.MessageID = messages.KeyLogsMigrationsApplied
	EventStorageReady   s99logger.MessageID = messages.KeyLogsStorageReady
	EventServerStarted  s99logger.MessageID = messages.KeyLogsServerStarted
	EventServerStopping s99logger.MessageID = messages.KeyLogsServerStopping
	EventNoIdentity     s99logger.MessageID = messages.KeyLogsNoIdentityProvider
	EventRunFailed      s99logger.MessageID = messages.KeyLogsRunFailed
)

// All is every event this server logs, for the test that proves each one has a
// message in every language.
var All = []s99logger.MessageID{
	EventConfigLoaded, EventMigrationsDone, EventStorageReady,
	EventServerStarted, EventServerStopping, EventNoIdentity, EventRunFailed,
}
