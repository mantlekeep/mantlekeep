package dev.mantlekeep.springboot.ai.agent;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

/**
 * The wire handling, tested without a network: request shape, non-streaming extraction, and
 * SSE delta parsing including the lines that must NOT become draft text.
 */
class HttpChatAgentTest {

    private static final ObjectMapper JSON = new ObjectMapper();

    @Test
    void requestCarriesModelAndPromptAndOmitsStreamWhenNotStreaming() throws Exception {
        JsonNode body = JSON.readTree(HttpChatAgent.body("gpt-4o", "draft the spec", false));

        assertEquals("gpt-4o", body.path("model").asText());
        assertEquals("user", body.path("messages").path(0).path("role").asText());
        assertEquals("draft the spec", body.path("messages").path(0).path("content").asText());
        assertTrue(body.path("stream").isMissingNode(), "non-streaming request must not set stream");
    }

    @Test
    void streamingRequestAsksForUsageSoTheDraftCanBeMetered() throws Exception {
        JsonNode body = JSON.readTree(HttpChatAgent.body("gpt-4o", "draft", true));

        assertTrue(body.path("stream").asBoolean());
        assertTrue(body.path("stream_options").path("include_usage").asBoolean());
    }

    @Test
    void promptWithQuotesAndNewlinesSurvivesEncoding() throws Exception {
        String awkward = "line one\n\"quoted\" and \\backslash\\";
        JsonNode body = JSON.readTree(HttpChatAgent.body("m", awkward, false));

        assertEquals(awkward, body.path("messages").path(0).path("content").asText());
    }

    @Test
    void extractsContentFromANonStreamingResponse() {
        String response = """
                {"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"# Spec\\n\\n- R1"}}]}
                """;
        assertEquals("# Spec\n\n- R1", HttpChatAgent.contentOf(response));
    }

    @Test
    void unexpectedResponseShapeYieldsEmptyRatherThanThrowing() {
        assertEquals("", HttpChatAgent.contentOf("{\"error\":\"boom\"}"));
        assertEquals("", HttpChatAgent.contentOf("not json at all"));
    }

    @Test
    void extractsDeltasFromSseLines() {
        String line = "data: {\"choices\":[{\"delta\":{\"content\":\" rate\"}}]}";
        assertEquals(" rate", HttpChatAgent.deltaOf(line));
    }

    @Test
    void linesThatCarryNoDraftTextAreIgnored() {
        assertNull(HttpChatAgent.deltaOf("data: [DONE]"), "the terminator is not draft text");
        assertNull(HttpChatAgent.deltaOf(""), "blank keep-alive lines are not draft text");
        assertNull(HttpChatAgent.deltaOf(": ping"), "SSE comments are not draft text");
        assertNull(HttpChatAgent.deltaOf("data: {\"choices\":[{\"delta\":{}}]}"), "an empty delta");
        assertNull(
                HttpChatAgent.deltaOf("data: {\"choices\":[],\"usage\":{\"total_tokens\":42}}"),
                "the usage-only final chunk must not leak into the draft");
    }
}
