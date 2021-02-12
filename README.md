# conjur-on-demand

conjur-on-demand is HTTP server that can communicate with a Docker daemon (local or remote), and uses it to create Conjur instances on demand. It works as follows:
0. Server receives `POST /conjur` request
1. Communicates to Docker using the Go library
2. Creates a Conjur and Postgres pair in their own network
3. Generates the data key in memory
4. Waits for Postgres to get ready before running Conjur
5. Waits for Conjur to be ready, then creates an account
6. Port maps Conjur 
7. Server responds with the admin API key and the host port where the new Conjur is listening

## Usage

Build and run:
```bash
➜  conjur-on-demand git:(master) ✗ go build -o conjur-on-demand ./...; ./conjur-on-demand
{"level":"info","msg":"Listening on :8000","time":"2021-02-12T22:27:01Z"}
2021/02/12 22:30:53 POST        /conjur ConjurCreate    26.516283165s
```

Make a request:
```bash
➜  ~ curl -X POST -v http://localhost:8000/conjur          
*   Trying 127.0.0.1...
* TCP_NODELAY set
* Connected to localhost (127.0.0.1) port 8000 (#0)
> POST /conjur HTTP/1.1
> Host: localhost:8000
> User-Agent: curl/7.54.0
> Accept: */*
> 
< HTTP/1.1 201 Created
< Content-Type: application/json; charset=UTF-8
< Date: Fri, 12 Feb 2021 22:30:53 GMT
< Content-Length: 110
< 
{"Id":"1613169026","AdminAPIKey":"evmagrypyjrz2qqtd4323ewtsn1b1csk01k47tv21pv0qte1chr17v","HostPort":"32770"}
* Connection #0 to host localhost left intact
```

Consume Conjur:
```bash
➜  ~ curl -v http://localhost:32770/info
*   Trying 127.0.0.1...
* TCP_NODELAY set
* Connected to localhost (127.0.0.1) port 32770 (#0)
> GET /info HTTP/1.1
> Host: localhost:32770
> User-Agent: curl/7.54.0
> Accept: */*
> 
< HTTP/1.1 401 Unauthorized
< Content-Type: text/plain
< Cache-Control: no-cache
< X-Request-Id: 4369ee9d-89ba-4482-ae45-e2f3ec4a02ee
< X-Runtime: 0.001081
< Content-Length: 21
< 
* Connection #0 to host localhost left intact
Authorization missing%
```

## TODOs

1. Clean up networks every X amount of time. Write network names to file so that server has persistence. Or use some docker thing, maybe labels.
2. Create delete endpoint.
