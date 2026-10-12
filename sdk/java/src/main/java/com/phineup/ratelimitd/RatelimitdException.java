package com.phineup.ratelimitd;

/** Non-2xx response other than 429. */
public class RatelimitdException extends RuntimeException {
    private final int status;

    public RatelimitdException(int status, String message) {
        super("ratelimitd: HTTP " + status + ": " + message);
        this.status = status;
    }

    public int status() {
        return status;
    }
}
