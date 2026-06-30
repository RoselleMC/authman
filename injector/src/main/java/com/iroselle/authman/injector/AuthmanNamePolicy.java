package com.iroselle.authman.injector;

public final class AuthmanNamePolicy {
    private static volatile boolean debug;

    private AuthmanNamePolicy() {
    }

    public static void configure(boolean debugEnabled) {
        debug = debugEnabled;
    }

    public static boolean validate(String name) {
        boolean allowed = isAllowedName(name);
        if (debug) {
            System.err.println("[AuthmanInjector] validate name=\"" + name + "\" allowed=" + allowed);
        }
        return allowed;
    }

    static boolean isAllowedName(String name) {
        if (name == null || name.isEmpty()) {
            return false;
        }
        if (name.length() > 16) {
            return false;
        }
        for (int offset = 0; offset < name.length();) {
            int codePoint = name.codePointAt(offset);
            if (!isAllowedCodePoint(codePoint)) {
                return false;
            }
            offset += Character.charCount(codePoint);
        }
        return true;
    }

    private static boolean isAllowedCodePoint(int codePoint) {
        if (codePoint == '_') {
            return true;
        }
        if (Character.isISOControl(codePoint)
            || Character.isWhitespace(codePoint)
            || Character.isSpaceChar(codePoint)) {
            return false;
        }
        int type = Character.getType(codePoint);
        if (type == Character.SURROGATE
            || type == Character.UNASSIGNED
            || type == Character.PRIVATE_USE
            || type == Character.FORMAT) {
            return false;
        }
        return Character.isLetterOrDigit(codePoint);
    }
}
