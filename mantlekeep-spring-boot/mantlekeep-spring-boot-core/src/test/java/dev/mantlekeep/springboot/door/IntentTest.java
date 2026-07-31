package dev.mantlekeep.springboot.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.util.HashMap;
import java.util.Map;
import org.junit.jupiter.api.Test;

class IntentTest {

    @Test
    void requiresAction() {
        assertThrows(IllegalArgumentException.class, () -> Intent.of("").goal("g").build());
    }

    @Test
    void requiresGoal() {
        assertThrows(IllegalArgumentException.class, () -> Intent.of("loop.propose").build());
    }

    @Test
    void defaultsOptionalStringFieldsToEmpty() {
        Intent i = Intent.of("loop.propose").goal("draft the spec").build();
        assertEquals("", i.resource());
        assertEquals("", i.env());
        assertEquals("", i.scope());
    }

    @Test
    void paramsAreDefensivelyCopiedAndUnmodifiable() {
        Map<String, Object> src = new HashMap<>();
        src.put("k", "v");
        Intent i = Intent.of("loop.propose").goal("g").params(src).build();

        src.put("leaked", "x"); // mutating the source must not affect the intent
        assertEquals(1, i.params().size());
        assertThrows(UnsupportedOperationException.class, () -> i.params().put("z", "1"));
    }
}
