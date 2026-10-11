// common/config/config.cue
package config

#Config: {
	application: #Application
	server:      #Server
	storage:     #Storage
	identity:    #Identity
	escrow:      #Escrow
	arrango:     #Arrango
	logging:     #Logging
	security:    #Security
	api:         #API
	dev:         #Dev
}

// Development-only surfaces. Every switch here is off in a real school.
#Dev: {
	// Registers POST /api/v1/plans/demo, which asks the first paired node for
	// Arrango's synthetic production-sized school and stores it as a project,
	// so the editor and the solver can be tried at full size.
	demo_school: bool | *false @go(DemoSchool)
}

// The solving nodes this deployment may dispatch to.
#Arrango: {
	// This deployment's own keypair. A node pins the certificate in here when
	// it enrols this server, so replacing the directory means every node must
	// pair again.
	identity_dir: string | *"./arrango-identity" @go(IdentityDir)
}

// The school's own key for .gpf Identity mode, held here so a new laptop does
// not destroy a year of timetables.
#Escrow: {
	// What encrypts the stored key at rest, through age's scrypt recipient.
	//
	// Empty disables escrow entirely: the key is never written rather than
	// written in the clear. A database dump is the threat this answers, and it
	// is a real one, because a .sql backup travels on a USB stick while the
	// configuration stays on the server. It does NOT protect against a
	// compromised server, which holds both halves by definition.
	//
	// Losing this passphrase loses every file the school ever exported in
	// Identity mode. It is not recoverable and nothing here can make it so.
	passphrase: string | *"@{env?:GOPTIVUM_ESCROW_PASSPHRASE}" @go(Passphrase,type="github.com/smegg99/s99config".Secret)
}

#Environment: "development" | "production" | "test"

#Application: {
	name:        string | *"goptivum-server"   @go(Name)
	environment: #Environment | *"development" @go(Environment)
	// What this program renders in: its log and what its command line prints.
	// A response never carries a sentence, only a key the desktop resolves in
	// the language the person reading it chose.
	language: "en" | "pl" | *"en" @go(Language)
	// The school this deployment belongs to, such as "szkola.edu.pl". It is
	// written into every .gpf this server exports and compared against the one
	// in a .gpf it is asked to open, so a file from another school is refused
	// with "this belongs to X and you are signed into Y" rather than a blank
	// failure.
	//
	// A message aid, never a security boundary: the header is plaintext and
	// unsigned, so a hostile file can claim any realm. What it cannot lie about
	// is which key opens it. Empty means no check.
	realm: string | *"" @go(Realm)
}

#Server: {
	// 8833 keeps the habit of staying off the ports other stacks on a
	// development machine already own.
	address:                  string | *"127.0.0.1:8833" @go(Address)
	read_timeout_seconds:     int & >0 & <=300 | *15      @go(ReadTimeoutSeconds)
	write_timeout_seconds:    int & >0 & <=300 | *30      @go(WriteTimeoutSeconds)
	idle_timeout_seconds:     int & >0 & <=600 | *60      @go(IdleTimeoutSeconds)
	shutdown_timeout_seconds: int & >0 & <=120 | *15      @go(ShutdownTimeoutSeconds)
	// Who may set X-Forwarded-For. Omitted, this is loopback only, which is
	// where a reverse proxy sits. An explicit [] is read differently: apimount
	// then trusts nothing and uses the peer address.
	//
	// type=[]string: a defaulted open list generates as []any otherwise, which
	// does not match apiv1.Options.TrustedProxies.
	trusted_proxies: [...string] | *["127.0.0.1", "::1"] @go(TrustedProxies,type=[]string)
}

#Database: {
	driver: "sqlite" | "postgres" | *"sqlite" @go(Driver)
	// A path for sqlite, a connection URL for postgres. The URL carries a
	// password, so it is a secret and never a command-line flag.
	dsn: string | *"@{env?:GOPTIVUM_DATABASE_URL}" @go(DSN,type="github.com/smegg99/s99config".Secret)
	// Where sqlite puts its file when no DSN says otherwise.
	path: string | *"./goptivum.db" @go(Path)
}

// Documents default to a local directory. A configured endpoint uses a bucket.
#Documents: {
	endpoint:   string | *""                @go(Endpoint)
	bucket:     string | *"goptivum-plans"  @go(Bucket)
	access_key: string | *"@{env?:GOPTIVUM_STORAGE_ACCESS_KEY}" @go(AccessKey,type="github.com/smegg99/s99config".Secret)
	secret_key: string | *"@{env?:GOPTIVUM_STORAGE_SECRET_KEY}" @go(SecretKey,type="github.com/smegg99/s99config".Secret)
	use_ssl:    bool | *false               @go(UseSSL)
	dir:        string | *"./documents"     @go(Dir)
}

#Storage: {
	database:  #Database
	documents: #Documents
}

// Ory Kratos. An empty public_url means this daemon has no identity provider:
// discovery and health still answer and every plan route stays unregistered,
// because mounting a guarded route without a session check mounts it open.
#Identity: {
	// Kratos's own public address, never this server's /kratos/ prefix. Routing
	// the session check back through our own proxy is a needless hop and a
	// recursion risk if the proxy target is ever pointed at itself.
	public_url: string | *"http://127.0.0.1:4833" @go(PublicURL)
	// Loopback only, always. Anything that reaches this port can read and
	// modify every identity in the school.
	admin_url: string | *"http://127.0.0.1:4834" @go(AdminURL)
	// Bounds every call to Kratos. A hung identity provider is an outage, and
	// an outage has to be reported rather than waited out.
	timeout_seconds: int & >0 & <=60 | *5 @go(TimeoutSeconds)
	// Bounds the /kratos/* proxy, which sits outside the versioned group and
	// so is never seen by the API's own limiter. An unlimited login route on a
	// school LAN is a password guessing machine.
	//
	// type=int: gengotypes makes every CUE int an int64 and httpmw wants a
	// plain int.
	rate_limit_per_minute: int & >0 | *60 @go(RateLimitPerMinute,type=int)
	// Kratos's own configuration file, read only to show its password rules on
	// Backend > Security: Kratos offers no way to read them while it runs.
	// Empty shows them as not known. Values Kratos takes from its environment
	// instead of this file are not seen.
	kratos_config: string | *"" @go(KratosConfig)
	bootstrap:     #Bootstrap
}

// The first admin. A fixed default password would be the worst thing this
// product could ship, so one is generated and printed exactly once.
#Bootstrap: {
	enabled:  bool | *true       @go(Enabled)
	username: string | *"admin"  @go(Username)
	// The identity schema requires an address; nothing is sent to it.
	email: string | *"admin@goptivum.invalid" @go(Email)
}

#Logging: {
	verbose:      bool | *false                                 @go(Verbose)
	no_color:     bool | *false                                 @go(NoColor)
	enable_files: bool | *false                                 @go(EnableFiles)
	dir:          string | *"./logs"                            @go(Dir)
	level:        "DEBUG" | "INFO" | "WARN" | "ERROR" | *"INFO" @go(Level)
	max_size_mb:  int & >0 | *10                                @go(MaxSizeMB)
	max_backups:  int & >=0 | *5                                @go(MaxBackups)
	max_age_days: int & >0 | *30                                @go(MaxAgeDays)
	log_name:     string | *"goptivum-server.log"               @go(LogName)
	compression:  "none" | "gzip" | "zstd" | *"zstd"            @go(Compression)
	local_time:   bool | *true                                  @go(LocalTime)
}

#Security: {
	// A plan endpoint carries a name and a school year, nothing more.
	maximum_body_bytes: int & >=1024 & <=10485760 | *1048576 @go(MaximumBodyBytes)
}

#API: {
	// The oldest Goptivum Desktop this server accepts. Raising it turns away
	// clients that already work.
	min_client_version: string | *"0.1.0" @go(MinClientVersion)
}
