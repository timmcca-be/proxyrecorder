# Proxy Recorder

Proxy Recorder is a proxy that records requests and responses based on
customizable criteria. The recorder also has the ability to take snapshots
after requests that mutate data are recorded. Snapshots are completely
customizable. Presumably the tool is customized by the user to create snapshots
of any relevant datastore entities.

Also included is a web client for viewing requests and responses as well as
snapshot diffs. 

The command included in this version of the tool is configured to record
Generalized Test Prep GraphQL requests, and it takes snapshots of all Test Prep
data for a given kaid and exam group using the GTP user data export facility.

## Running the tool

Running the tool is simple:

1. Start the dev server.
2. Create a dev user and note the user's kaid.
3. Start the proxy recorder.

```
go run cmd/proxyrecorder/main.go output ~/khan/webapp <kaid> lsat
```

NOTE: you must run this command in the root of the proxyrecorder repo.

The first argument is the directory where requests are recorded. The next
argument is the location of the webapp repo (needed to run the GTP user data
export tool). After that, you specify the kaid of the dev user and the exam
group you'll be recording requests for (snapshots are taken for the specified
exam group).

After running this command you will see the output:

```
tool:  listening on http://127.0.0.1:1234
proxy: listening on http://127.0.0.1:7081
```

Open http://127.0.0.1:1234 to view the proxy recorder interface. Then, to start
recording requests, go to http://127.0.0.1:7081. All requests sent to
http://127.0.0.1:7081 are proxied to localhost:8081. 

Note that if you go directly to localhost:8081 and perform any actions that
mutate data, the snapshot diffs will be incorrect since the proxy snapshots
will include differences made by requests that weren't recorded.
