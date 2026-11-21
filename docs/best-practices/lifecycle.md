# Process lifecycle

`appserver.Start` creates the `mcode app-server` process and owns it. `Close` is the normal way to release that process. `Close` is safe to call more than once.

`Close` closes the client connection, then signals the child. On Unix, `Start` puts the child in its own process group. `Close` signals that group, including descendants that are still in the group after the leader has exited. On Linux, `Close` checks `/proc/<pid>/stat` start time before it signals. If that pid now belongs to a different process, `Close` does not signal it.

On Linux, `Start` sets `Pdeathsig` to `SIGKILL` on the thread that starts the child. That thread stays locked until `Close`, or until the child is reaped. An idle Go thread exiting does not kill the server. If the owner process itself exits, the kernel signals the child.

macOS and other Unix systems do not set `Pdeathsig`. If the owner process is killed and `Close` never runs, those systems do not promise that the child exits. Windows `Close` kills the direct child process. It does not walk a process tree.

macOS and other Unix systems signal the process group when it still exists. They do not have the Linux start-time check. A process group id is not reused while a member remains. After the group is empty, a later `Close` does not signal it.

`DialStdio` does not own a process. `Close` closes the streams you passed. It does not kill a server you started yourself.

`Close` does not call `AbortSession`. Stopping the owned process ends a running turn because the server process exits.
