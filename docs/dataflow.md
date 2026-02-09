┌──────────┐         ┌─────────────────────────────────────┐         ┌──────────┐
│  Browser │         │            PROXY SERVER             │         │  Origin  │
│          │         │                                     │         │  Server  │
└────┬─────┘         │  ┌─────────┐  ┌───────┐  ┌───────┐  │         └────┬─────┘
     │               │  │Blocklist│  │ Cache │  │ Stats │  │              │
     │   REQUEST     │  └────┬────┘  └───┬───┘  └───┬───┘  │              │
     │───────────────┼──────►│           │          │      │              │
     │               │       ▼           │          │      │              │
     │               │   Blocked? ───YES─┼──► 403   │      │              │
     │               │       │ NO        │          │      │              │
     │               │       ▼           │          │      │              │
     │               │   In cache? ──YES─┼──► Return│      │              │
     │               │       │ NO        │   cached │      │              │
     │               │       ▼           │          │      │   REQUEST    │
     │               │   Forward ────────┼──────────┼──────┼─────────────►│
     │               │                   │          │      │              │
     │               │                   │          │      │   RESPONSE   │
     │               │   Store in ◄──────┼──────────┼──────┼──────────────│
     │   RESPONSE    │   cache           │          │      │              │
     │◄──────────────┼───────────────────┘          │      │              │
     │               │                              │      │              │
     │               └──────────────────────────────┴──────┘              │
The program should be able to:
1. Respond to HTTP & HTTPS requests and should display each request on a management
console. It should forward the request to the Web server and relay the response to the browser.
2. Dynamically block selected URLs via the management console.
3. Efficiently cache HTTP requests locally and thus save bandwidth. You must gather timing data
to prove the efficiency of your proxy.
4. Handle multiple requests simultaneously by implementing a threaded server.
![alt text](image.png)     