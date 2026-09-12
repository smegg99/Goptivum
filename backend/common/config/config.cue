// common/config/config.cue
package config

#Config: {
	application: #Application
	server:      #Server
	storage:     #Storage
	logging:     #Logging
	security:    #Security
	api:         #API
}

#Environment: "development" | "production" | "test"

#Application: {
	name:        string | *"goptivum-server"   @go(Name)
	environment: #Environment | *"development" @go(Environment)
	// What this program renders in: its log and what its command line prints.
	// A response never carries a sentence, only a key the desktop resolves in
	// the language the person reading it chose.
	language: "en" | "pl" | *"en" @go(Language)
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

// Documents live in a bucket by default and in a directory for a single-box or
// air-gapped school. An empty endpoint selects the directory.
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
	// A real Optivum file is tens of kilobytes and a document about 25 KB
	// compressed, so this is headroom rather than a limit anyone meets.
	maximum_upload_bytes: int & >=1024 & <=268435456 | *16777216 @go(MaximumUploadBytes)
	// type=int: gengotypes defaults every CUE int to int64, and
	// apimount.Options.RateLimitPerMinute is a plain int.
	rate_limit_per_minute: int & >=0 | *600 @go(RateLimitPerMinute,type=int)
}

#API: {
	// The oldest Goptivum Desktop this server accepts. Raising it turns away
	// clients that already work.
	min_client_version: string | *"0.1.0" @go(MinClientVersion)
}
