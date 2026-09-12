// i18n/config.cue
defaultLocale: "en"
locales: ["en", "pl"]
// This server renders two things a person reads: its log, and what its command
// line prints. Everything a response carries is a key the desktop resolves from
// its own catalogue, so no web target lives here.
targets: ["backend"]
roots: ["locales"]
output: "generated"
namespaces: {
	logs: {targets: ["backend"]}
	cli: {targets: ["backend"]}
}
scan: ["../backend/common/logger", "../backend/cmd", "../tools/plaprobe"]
go: {package: "messages"}
