package dev.mantlekeep.core;

import dev.mantlekeep.door.NativeCore;
import java.lang.foreign.Arena;
import java.lang.foreign.FunctionDescriptor;
import java.lang.foreign.Linker;
import java.lang.foreign.MemorySegment;
import java.lang.foreign.SymbolLookup;
import java.lang.foreign.ValueLayout;
import java.lang.invoke.MethodHandle;
import java.nio.file.Path;
import java.util.List;

/**
 * The Panama (FFM) binding over the Rust core's C ABI — the {@code java-core} of
 * composition-model §4c, adapted from the proven rust-core spike
 * ({@code spike/rust-core/java-core/MantlekeepCoreLibrary.java}; production path: the
 * UniFFI generator over the identical exported surface). The core runs IN-PROCESS:
 * no subprocess, no wire — the embedded-DB pattern. The FFI surface is SACRED:
 * five symbols, versioned with the core, never sprawling.
 */
public final class PanamaMantlekeepCore implements NativeCore {

    private final Arena arena = Arena.ofShared();
    private final MemorySegment door;
    private final MethodHandle submitJsonHandle;
    private final MethodHandle auditJsonHandle;
    private final MethodHandle verifyHandle;
    private final MethodHandle doorFreeHandle;
    private final MethodHandle stringFreeHandle;

    public PanamaMantlekeepCore(Path coreLibraryPath, List<String> policyPaths) {
        Linker linker = Linker.nativeLinker();
        SymbolLookup coreLibrary = SymbolLookup.libraryLookup(coreLibraryPath, arena);
        MethodHandle doorNewHandle = downcall(linker, coreLibrary, "mantle_door_new",
                FunctionDescriptor.of(ValueLayout.ADDRESS, ValueLayout.ADDRESS));
        submitJsonHandle = downcall(linker, coreLibrary, "mantle_door_submit_json",
                FunctionDescriptor.of(ValueLayout.ADDRESS, ValueLayout.ADDRESS, ValueLayout.ADDRESS));
        auditJsonHandle = downcall(linker, coreLibrary, "mantle_door_audit_json",
                FunctionDescriptor.of(ValueLayout.ADDRESS, ValueLayout.ADDRESS));
        verifyHandle = downcall(linker, coreLibrary, "mantle_door_verify",
                FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.ADDRESS));
        doorFreeHandle = downcall(linker, coreLibrary, "mantle_door_free",
                FunctionDescriptor.ofVoid(ValueLayout.ADDRESS));
        stringFreeHandle = downcall(linker, coreLibrary, "mantle_string_free",
                FunctionDescriptor.ofVoid(ValueLayout.ADDRESS));

        try (Arena callArena = Arena.ofConfined()) {
            MemorySegment policyPathsJson = callArena.allocateFrom(jsonArrayOf(policyPaths));
            MemorySegment createdDoor = (MemorySegment) doorNewHandle.invoke(policyPathsJson);
            if (createdDoor.equals(MemorySegment.NULL)) {
                throw new IllegalStateException(
                        "the Rust core rejected the policy documents: " + policyPaths);
            }
            door = createdDoor;
        } catch (Throwable failure) {
            throw asRuntime(failure);
        }
    }

    @Override
    public String submitJson(String intentJson) {
        try (Arena callArena = Arena.ofConfined()) {
            MemorySegment input = callArena.allocateFrom(intentJson);
            return readAndFree((MemorySegment) submitJsonHandle.invoke(door, input));
        } catch (Throwable failure) {
            throw asRuntime(failure);
        }
    }

    @Override
    public String auditJson() {
        try {
            return readAndFree((MemorySegment) auditJsonHandle.invoke(door));
        } catch (Throwable failure) {
            throw asRuntime(failure);
        }
    }

    @Override
    public boolean verifyChain() {
        try {
            return (int) verifyHandle.invoke(door) == 1;
        } catch (Throwable failure) {
            throw asRuntime(failure);
        }
    }

    @Override
    public void close() {
        try {
            doorFreeHandle.invoke(door);
        } catch (Throwable failure) {
            throw asRuntime(failure);
        } finally {
            arena.close();
        }
    }

    private String readAndFree(MemorySegment returned) throws Throwable {
        if (returned.equals(MemorySegment.NULL)) {
            throw new IllegalStateException("the Rust core returned NULL (malformed payload?)");
        }
        String text = returned.reinterpret(Long.MAX_VALUE).getString(0);
        stringFreeHandle.invoke(returned);
        return text;
    }

    private static MethodHandle downcall(Linker linker, SymbolLookup coreLibrary, String symbol,
                                         FunctionDescriptor descriptor) {
        return linker.downcallHandle(
                coreLibrary.find(symbol)
                        .orElseThrow(() -> new IllegalStateException("missing symbol " + symbol)),
                descriptor);
    }

    /** A JSON array of strings — kept local so this thin binding stays dependency-free. */
    private static String jsonArrayOf(List<String> values) {
        StringBuilder out = new StringBuilder("[");
        for (int index = 0; index < values.size(); index++) {
            if (index > 0) {
                out.append(',');
            }
            out.append('"')
                    .append(values.get(index).replace("\\", "\\\\").replace("\"", "\\\""))
                    .append('"');
        }
        return out.append(']').toString();
    }

    private static RuntimeException asRuntime(Throwable failure) {
        return failure instanceof RuntimeException runtime
                ? runtime
                : new IllegalStateException(failure);
    }
}
