#include <xpc/xpc.h>
#include <stdint.h>

extern void goHandleProgressUpdate(uint64_t id, xpc_object_t object);

// This wrapper handles the GCD block syntax away from CGO's eyes
void set_empty_xpc_handler(xpc_connection_t conn) {
    xpc_connection_set_event_handler(conn, ^(xpc_object_t object) {
        // You can leave this empty if you only care about synchronous replies,
        // but libxpc requires *some* block handler to be registered before resuming.
    });
}

xpc_connection_t create_progress_endpoint(uint64_t id, xpc_endpoint_t *endpoint_out) {
    xpc_connection_t listener = xpc_connection_create(NULL, NULL);
    __block xpc_connection_t reverse = NULL;
    xpc_connection_set_event_handler(listener, ^(xpc_object_t object) {
        xpc_type_t type = xpc_get_type(object);
        if (type == XPC_TYPE_CONNECTION) {
            reverse = object;
            xpc_connection_set_event_handler(object, ^(xpc_object_t update) {
                if (xpc_get_type(update) == XPC_TYPE_DICTIONARY) {
                    goHandleProgressUpdate(id, update);
                }
            });
            xpc_connection_resume(object);
        } else if (type == XPC_TYPE_ERROR) {
            if (reverse != NULL) {
                xpc_connection_cancel(reverse);
                reverse = NULL;
            }
        }
    });
    xpc_connection_resume(listener);
    *endpoint_out = xpc_endpoint_create(listener);
    return listener;
}
