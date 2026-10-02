// common/config/settings.cue
package config

import (
	"list"
	"strings"
)

// What an administrator changes while the server runs. It lives in its own
// file beside config.yaml, which the server writes and never the operator,
// so a change made in the desktop cannot collide with an edit of config.yaml.
#Settings: {
	lockout:  #Lockout
	server:   #ServerSettings
	projects: #Projects
}

// The sign-in lockout the /kratos proxy enforces. Kratos has none of its own.
// A change applies at the next sign-in.
//
// type=int: gengotypes makes every CUE int an int64.
#Lockout: {
	// Failed sign-ins with one sign-in name before that name is blocked. The
	// name is counted whether or not an account has it.
	account_failures: int & >=1 & <=100 | *5 @go(AccountFailures,type=int)
	// Failed sign-ins from one client address, with any names, before the
	// address is blocked. Higher than the name's, because a school's machines
	// may share one address.
	address_failures: int & >=1 & <=1000 | *20 @go(AddressFailures,type=int)
	// How long a block lasts, and how far back failures are counted.
	block_minutes: int & >=1 & <=1440 | *15 @go(BlockMinutes,type=int)
}

// How the server meets its clients.
#ServerSettings: {
	// The other addresses this same server answers on, published at
	// /api/info so a client that connected locally learns the public one.
	// Read on every request.
	//
	// Not derived, because this server cannot see itself from outside: what is
	// reachable from the internet is a decision about the school's router, and
	// only the person who made it knows the answer.
	public_addresses: [...#PublicAddress] & list.MaxItems(16) | *[] @go(PublicAddresses,type=[]string)
	// A real Optivum file is tens of kilobytes and a document about 25 KB
	// compressed, so this is headroom rather than a limit anyone meets. Read
	// once at start: the upload routes' cap is built into the router.
	maximum_upload_bytes: int & >=1024 & <=268435456 | *16777216 @go(MaximumUploadBytes)
	// Requests one client address may make to /api/v1 per minute; 0 is off.
	// Read once at start: the limiter is built into the router.
	//
	// type=int: apimount.Options.RateLimitPerMinute is a plain int.
	rate_limit_per_minute: int & >=0 & <=100000 | *600 @go(RateLimitPerMinute,type=int)
}

// An absolute http or https URL with a host and no user, query or fragment.
#PublicAddress: string & strings.MaxRunes(255) & =~#"^https?://[^\s/?#@]+(/[^\s?#]*)?$"#

// What happens to a school's projects over time. Each is read when it is
// used, so a change applies at once.
#Projects: {
	// How long a deleted project, checkpoint or result stays in Trash before
	// it is removed for good. The removal date shown in Trash is the deletion
	// time plus this, so changing it moves every date, including those
	// already in Trash.
	//
	// type=int: gengotypes makes every CUE int an int64.
	trash_days: int & >0 & <=3650 | *30 @go(TrashDays,type=int)
	// How many older versions of a project's main file are kept in Backups/,
	// checkpoints not counted. Each new version removes the oldest beyond this
	// for good. A project can set its own.
	backups_keep: int & >0 & <=10000 | *100 @go(BackupsKeep,type=int)
	// How many unpinned solve results a project keeps in its results folder.
	// Each new result removes the oldest unpinned ones beyond this for good;
	// pinned results and results in Trash are not counted. A project can set
	// its own.
	results_keep: int & >0 & <=10000 | *20 @go(ResultsKeep,type=int)
}
