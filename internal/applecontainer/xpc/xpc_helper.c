#include <xpc/xpc.h>

// This wrapper handles the GCD block syntax away from CGO's eyes
void set_empty_xpc_handler(xpc_connection_t conn) {
    xpc_connection_set_event_handler(conn, ^(xpc_object_t object) {
        // You can leave this empty if you only care about synchronous replies,
        // but libxpc requires *some* block handler to be registered before resuming.
    });
}