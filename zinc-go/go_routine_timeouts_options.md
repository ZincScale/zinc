Right — that's the actual remaining problem. Even with a handle, a goroutine doing nothing visible is indistinguishable from one doing real long-running work. Detection is the hard part.                                                  
                                                            
  The honest options                                                                                                                                                                                                                          
   
  1. Mandatory timeouts at the language level (zinc enforces)                                                                                                                                                                                 
  - Every blocking op requires a deadline:                  
  concurrent(timeout=10s) { ... }                                                                                                                                                                                                             
  ch.recv(timeout=1s)                                       
  lock(mu, timeout=500ms) { ... }                                                                                                                                                                                                             
  t.wait(5s)                     
  - If a deadline expires → the operation fails / cancels / raises. "Hung" becomes a first-class outcome the user has to handle.                                                                                                              
  - Trade: indefinite waits (servers, event loops, long polls) need an explicit escape — wait_forever keyword that's deliberately ugly + linted.
                                                                                                                                                                                                                                              
  2. Watchdog instrumentation at codegen time                                                                                                                                                                                                 
  - zinc emits spawn { body } as spawn { trackedBody(my_id, body) } with a heartbeat / progress channel.                                                                                                                                      
  - A single watchdog goroutine reads heartbeats, reports "no progress from goroutine X for N seconds."                                                                                                                                       
  - Trade: overhead per spawn; false positives for legit long ops; needs cooperation if body doesn't naturally tick.                                                                                                                          
                                                                                                                                                                                                                                              
  3. Stack profiling at runtime                                                                                                                                                                                                               
  - Periodic runtime.Stack() snapshots, compare across samples — goroutines stuck on the same call site for N samples → flag.                                                                                                                 
  - Trade: passive but expensive; false positives for goroutines legitimately blocked on a long syscall.                                                                                                                                      
                                                            
  4. Platform observability                                                                                                                                                                                                                   
  - pprof, Prometheus goroutine count, distributed traces. Out-of-band, post-hoc.
  - Trade: detection happens after the fact, not at the operation site.                                                                                                                                                                       
                                                                       
  Honest recommendation                                                                                                                                                                                                                       
                                                                                                                                                                                                                                              
  Mandatory timeouts (option 1) is the only one that does detection at the source, where the user can act on it. Watchdog and profiling tell you "something is stuck somewhere"; only timeouts tell you "this specific operation didn't       
  complete in time."                                                                                                                                                                                                                          
                                                                                                                                                                                                                                              
  The language-level rule:                                  
  - Every blocking primitive (recv, lock, wait, concurrent) takes a timeout parameter.
  - The default value is not infinite — there is no default. The user types a duration explicitly.                                                                                                                                            
  - An explicit forever keyword exists for the case where the user really means "wait until shutdown" (servers, long polls). Lint warns on its use; CI can fail on forever outside an allowlisted file.
                                                                                                                                                                                                                                              
  Same enforcement shape as & and the spawn-owner rule: parser is permissive, validator runs after parse, compile error if a blocking op lacks a timeout.                                                                                     
                                                                                                                                                                                                                                              
  That's a real architectural design — and it's a rule zinc can enforce regardless of target. zinc-go emits select { case <-ch: ; case <-time.After(d): }. zinc-csharp emits await ch.Reader.ReadAsync(cts.Token).WaitAsync(d). zinc-java     
  emits BlockingQueue.poll(d, MILLISECONDS).                                                                                                                                                                                                  
                                                                                                                                                                                                                                              
  Worth pursuing? Or is the watchdog/runtime-detection angle more what you had in mind?     
