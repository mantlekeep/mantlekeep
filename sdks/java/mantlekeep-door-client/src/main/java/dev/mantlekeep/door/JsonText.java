package dev.mantlekeep.door;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * Minimal JSON text helpers for the door's small, known payloads — build with
 * escaping, read with patterns. Deliberately dependency-free: the repo standard
 * prefers the JDK over pulling a JSON library onto the host's classpath for a
 * surface this small. (Adapted from the rust-core spike's helper.)
 */
final class JsonText {

    private JsonText() {
    }

    /** Escapes a string for embedding in a JSON document. */
    static String quote(String value) {
        StringBuilder out = new StringBuilder("\"");
        for (char character : value.toCharArray()) {
            switch (character) {
                case '"' -> out.append("\\\"");
                case '\\' -> out.append("\\\\");
                case '\n' -> out.append("\\n");
                case '\r' -> out.append("\\r");
                case '\t' -> out.append("\\t");
                default -> {
                    if (character < 0x20) {
                        out.append(String.format("\\u%04x", (int) character));
                    } else {
                        out.append(character);
                    }
                }
            }
        }
        return out.append('"').toString();
    }

    /** Renders a JSON array of strings. */
    static String array(List<String> values) {
        return values.stream().map(JsonText::quote).collect(Collectors.joining(",", "[", "]"));
    }

    /** Renders a JSON object of string→string. */
    /**
     * Serialises a parameter map, including NESTED structures.
     *
     * <p>Nesting is not a convenience: a policy floor that caps a set of resources reads a
     * MAP at its parameter — {@code {"maxResources": {"cpu": "8"}}} — and a flat
     * string-to-string map cannot express that. A client limited to flat values cannot
     * trigger such a floor at all, and the failure is silent: the request is allowed
     * because the floor found nothing to inspect.
     */
    static String object(Map<String, ?> values) {
        return values.entrySet().stream()
                .map(entry -> quote(entry.getKey()) + ":" + value(entry.getValue()))
                .collect(Collectors.joining(",", "{", "}"));
    }

    /**
     * Renders one value. Numbers and booleans keep their JSON types so a numeric floor
     * compares numbers; maps and lists recurse; anything else is quoted as a string.
     */
    @SuppressWarnings("unchecked")
    private static String value(Object raw) {
        if (raw == null) {
            return "null";
        }
        if (raw instanceof Map<?, ?> nested) {
            return object((Map<String, ?>) nested);
        }
        if (raw instanceof Iterable<?> items) {
            StringBuilder out = new StringBuilder("[");
            boolean first = true;
            for (Object item : items) {
                if (!first) {
                    out.append(',');
                }
                out.append(value(item));
                first = false;
            }
            return out.append(']').toString();
        }
        if (raw instanceof Number || raw instanceof Boolean) {
            return raw.toString();
        }
        return quote(raw.toString());
    }

    /** Extracts a string field's value (unescaped), or empty when absent. */
    static String stringField(String json, String field) {
        Matcher matcher = Pattern
                .compile("\"" + Pattern.quote(field) + "\"\\s*:\\s*\"((?:[^\"\\\\]|\\\\.)*)\"")
                .matcher(json);
        return matcher.find() ? matcher.group(1).replace("\\\"", "\"").replace("\\\\", "\\") : "";
    }

    /** Extracts a boolean field, or the fallback when absent. */
    static boolean booleanField(String json, String field, boolean fallback) {
        Matcher matcher = Pattern
                .compile("\"" + Pattern.quote(field) + "\"\\s*:\\s*(true|false)")
                .matcher(json);
        return matcher.find() ? Boolean.parseBoolean(matcher.group(1)) : fallback;
    }

    /**
     * Extracts a named array field's raw {@code [...]} text (bracket counting,
     * string-aware), or {@code "[]"} when absent — the wire's audit view wraps its
     * records in an envelope object.
     */
    static String arrayField(String json, String field) {
        Matcher matcher = Pattern
                .compile("\"" + Pattern.quote(field) + "\"\\s*:\\s*\\[")
                .matcher(json);
        if (!matcher.find()) {
            return "[]";
        }
        int arrayStart = matcher.end() - 1;
        int depth = 0;
        boolean insideString = false;
        boolean escaped = false;
        for (int index = arrayStart; index < json.length(); index++) {
            char character = json.charAt(index);
            if (escaped) {
                escaped = false;
                continue;
            }
            if (character == '\\') {
                escaped = true;
                continue;
            }
            if (character == '"') {
                insideString = !insideString;
                continue;
            }
            if (insideString) {
                continue;
            }
            if (character == '[') {
                depth++;
            } else if (character == ']') {
                depth--;
                if (depth == 0) {
                    return json.substring(arrayStart, index + 1);
                }
            }
        }
        return "[]";
    }

    /**
     * Extracts the unescaped string elements of a JSON array of strings — the shape
     * {@code requiredApprovers} arrives in. Objects inside the array are ignored; this
     * reads scalar strings only, which is all a list of role names is.
     */
    static List<String> stringsOfArray(String json) {
        List<String> values = new ArrayList<>();
        Matcher matcher = Pattern.compile("\"((?:[^\"\\\\]|\\\\.)*)\"").matcher(json);
        while (matcher.find()) {
            values.add(matcher.group(1).replace("\\\"", "\"").replace("\\\\", "\\"));
        }
        return values;
    }

    /**
     * Splits a JSON array of objects into the objects' raw text, by brace counting
     * (string-aware). Enough for the door's flat audit records; not a general parser.
     */
    static List<String> objectsOfArray(String json) {
        List<String> objects = new ArrayList<>();
        int depth = 0;
        int objectStart = -1;
        boolean insideString = false;
        boolean escaped = false;
        for (int index = 0; index < json.length(); index++) {
            char character = json.charAt(index);
            if (escaped) {
                escaped = false;
                continue;
            }
            if (character == '\\') {
                escaped = true;
                continue;
            }
            if (character == '"') {
                insideString = !insideString;
                continue;
            }
            if (insideString) {
                continue;
            }
            if (character == '{') {
                if (depth == 0) {
                    objectStart = index;
                }
                depth++;
            } else if (character == '}') {
                depth--;
                if (depth == 0 && objectStart >= 0) {
                    objects.add(json.substring(objectStart, index + 1));
                    objectStart = -1;
                }
            }
        }
        return objects;
    }
}
