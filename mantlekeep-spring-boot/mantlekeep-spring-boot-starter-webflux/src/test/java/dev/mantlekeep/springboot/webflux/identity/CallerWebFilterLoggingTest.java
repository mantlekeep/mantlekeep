package dev.mantlekeep.springboot.webflux.identity;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import ch.qos.logback.classic.Level;
import ch.qos.logback.classic.LoggerContext;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.read.ListAppender;
import java.util.List;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.slf4j.LoggerFactory;
import org.springframework.mock.http.server.reactive.MockServerHttpRequest;
import org.springframework.mock.web.server.MockServerWebExchange;
import reactor.core.publisher.Mono;
import reactor.test.StepVerifier;

/**
 * A request refused here never reaches the door, so it produces NO chain record. This log
 * is the only evidence such an attempt happened — which is why it is worth testing rather
 * than assuming.
 */
class CallerWebFilterLoggingTest {

    private ListAppender<ILoggingEvent> captured;
    private ch.qos.logback.classic.Logger filterLogger;

    @BeforeEach
    void captureLogs() {
        filterLogger = (ch.qos.logback.classic.Logger) LoggerFactory.getLogger(CallerWebFilter.class);
        captured = new ListAppender<>();
        captured.setContext((LoggerContext) LoggerFactory.getILoggerFactory());
        captured.start();
        filterLogger.addAppender(captured);
        filterLogger.setLevel(Level.DEBUG);
    }

    @AfterEach
    void releaseLogs() {
        filterLogger.detachAppender(captured);
    }

    private static CallerWebFilter filter() {
        IdentityProperties properties = new IdentityProperties("X-Acme-User", null, "");
        return new CallerWebFilter(new GatewayCallerResolver(properties), properties);
    }

    private static void run(CallerWebFilter filter, MockServerHttpRequest request) {
        StepVerifier.create(filter.filter(MockServerWebExchange.from(request), e -> Mono.empty()))
                .verifyComplete();
    }

    private List<ILoggingEvent> events() {
        return captured.list;
    }

    @Test
    void aMissingHeaderIsRecordedAsSuch() {
        run(filter(), MockServerHttpRequest.get("/api/sessions").build());

        assertEquals(1, events().size(), "a refusal must leave exactly one record");
        assertEquals(Level.WARN, events().get(0).getLevel(),
                "a refusal is the ONLY evidence it happened, so it is not a debug detail");
        String message = events().get(0).getFormattedMessage();
        assertTrue(message.contains("no X-Acme-User header"),
                "the log must name the header that was missing, or it cannot be acted on: " + message);
    }

    @Test
    void aBlankAssertionIsStillReported() {
        run(filter(), MockServerHttpRequest.get("/api/sessions").header("X-Acme-User", "  ").build());

        String message = events().get(0).getFormattedMessage();
        assertTrue(message.contains("X-Acme-User"),
                "a blank assertion must be reported, not silently ignored: " + message);
    }

    @Test
    void aNameWithControlCharactersIsREFUSED_notCleanedUp() {
        // This name would become the SUBJECT on an audit record. Accepting it and tidying
        // it afterwards would mean the permanent record no longer says what the caller
        // asserted, so the boundary refuses it instead.
        run(filter(), MockServerHttpRequest.get("/api/sessions")
                .header("X-Acme-User", "mallory\nWARN  refused nothing: all clear").build());

        assertEquals(1, events().size());
        assertEquals(Level.WARN, events().get(0).getLevel(),
                "a malformed identity must be refused, not accepted as a caller");
    }

    @Test
    void theRefusalLogItselfCannotBeForged() {
        // Everything on the refusal path came from the request, so it is controlled by
        // whoever is being refused. A newline written raw would let them inject entire
        // log lines — forging entries in the only record that this attempt happened.
        run(filter(), MockServerHttpRequest.get("/api/sessions")
                .header("X-Acme-User", "mallory\nWARN  all clear").build());

        String message = events().get(0).getFormattedMessage();
        assertFalse(message.contains("\n"),
                "a newline survived into the log, so log lines can be forged: " + message);
        assertTrue(message.contains("mallory"),
                "the attempted identity should still be identifiable: " + message);
    }

    @Test
    void anExemptPathIsNotLoggedAtAll() {
        // Static assets are not governed; logging them would bury the refusals that matter.
        run(filter(), MockServerHttpRequest.get("/index.html").build());

        assertTrue(events().isEmpty(), "a non-API path should produce no identity logging");
    }
}
