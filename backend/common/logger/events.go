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

	EventIdentityReady         s99logger.MessageID = messages.KeyLogsIdentityReady
	EventBootstrapAdminCreated s99logger.MessageID = messages.KeyLogsBootstrapAdminCreated
	EventBootstrapAdminExists  s99logger.MessageID = messages.KeyLogsBootstrapAdminExists
	EventBootstrapFailed       s99logger.MessageID = messages.KeyLogsBootstrapFailed
	EventOrphanedJobsClosed    s99logger.MessageID = messages.KeyLogsOrphanedJobsClosed
	EventEscrowKeyReady        s99logger.MessageID = messages.KeyLogsEscrowKeyReady
	EventEscrowDisabled        s99logger.MessageID = messages.KeyLogsEscrowDisabled

	EventTrashPurged       s99logger.MessageID = messages.KeyLogsTrashPurged
	EventTrashPurgeFailed  s99logger.MessageID = messages.KeyLogsTrashPurgeFailed
	EventObjectsSwept      s99logger.MessageID = messages.KeyLogsObjectsSwept
	EventObjectSweepFailed s99logger.MessageID = messages.KeyLogsObjectSweepFailed
	EventDemoSchoolEnabled s99logger.MessageID = messages.KeyLogsDemoSchoolEnabled
	EventDemoSchoolFailed  s99logger.MessageID = messages.KeyLogsDemoSchoolFailed

	EventCheckDone        s99logger.MessageID = messages.KeyLogsCheckDone
	EventCheckFailed      s99logger.MessageID = messages.KeyLogsCheckFailed
	EventCheckCountsDrift s99logger.MessageID = messages.KeyLogsCheckCountsDrift

	EventRuleCatalogueFailed s99logger.MessageID = messages.KeyLogsRuleCatalogueFailed

	EventExportFailed s99logger.MessageID = messages.KeyLogsExportFailed

	EventNodePairingRefused s99logger.MessageID = messages.KeyLogsNodePairingRefused

	EventSignInBlocked           s99logger.MessageID = messages.KeyLogsSignInBlocked
	EventLockoutChanged          s99logger.MessageID = messages.KeyLogsLockoutChanged
	EventUserSessionsEnded       s99logger.MessageID = messages.KeyLogsUserSessionsEnded
	EventPasswordRulesUnreadable s99logger.MessageID = messages.KeyLogsPasswordRulesUnreadable

	EventServerSettingsChanged s99logger.MessageID = messages.KeyLogsServerSettingsChanged
	EventTrashEmptied          s99logger.MessageID = messages.KeyLogsTrashEmptied
	EventBackupsTrimmed        s99logger.MessageID = messages.KeyLogsBackupsTrimmed
	EventOwnersUnread          s99logger.MessageID = messages.KeyLogsOwnersUnread
	EventAuditFailed           s99logger.MessageID = messages.KeyLogsAuditFailed
)

// All is every event this server logs, for the test that proves each one has a
// message in every language.
var All = []s99logger.MessageID{
	EventConfigLoaded, EventMigrationsDone, EventStorageReady,
	EventServerStarted, EventServerStopping, EventNoIdentity, EventRunFailed,
	EventIdentityReady, EventBootstrapAdminCreated, EventBootstrapAdminExists,
	EventBootstrapFailed, EventOrphanedJobsClosed,
	EventEscrowKeyReady, EventEscrowDisabled,
	EventTrashPurged, EventTrashPurgeFailed, EventObjectsSwept, EventObjectSweepFailed,
	EventDemoSchoolEnabled, EventDemoSchoolFailed,
	EventCheckDone, EventCheckFailed, EventCheckCountsDrift,
	EventRuleCatalogueFailed,
	EventExportFailed,
	EventNodePairingRefused,
	EventSignInBlocked, EventLockoutChanged, EventUserSessionsEnded, EventPasswordRulesUnreadable,
	EventServerSettingsChanged, EventTrashEmptied, EventBackupsTrimmed, EventOwnersUnread,
	EventAuditFailed,
}
