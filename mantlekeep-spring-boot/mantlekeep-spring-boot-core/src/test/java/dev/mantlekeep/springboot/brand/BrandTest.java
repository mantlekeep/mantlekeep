package dev.mantlekeep.springboot.brand;

import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.Test;

/** An unbranded build is honestly MantleKeep; a branded one wears exactly what it was given. */
class BrandTest {

    @Test
    void defaultsToTheEngineName_soAnUnbrandedBuildIsHonestlyMantlekeep() {
        Brand b = Brand.DEFAULT;
        assertEquals("MantleKeep", b.name());
        assertEquals("🔱", b.mark());
        assertEquals("the one door — human + AI, every action governed", b.tagline());
        assertEquals("", b.kicker());
    }

    @Test
    void blankFieldsFallBack_butProvidedOnesAreKept() {
        Brand b = new Brand("MantleKeep", "◇", "  ", null); // blank tagline, null kicker
        assertEquals("MantleKeep", b.name());
        assertEquals("◇", b.mark());
        assertEquals("the one door — human + AI, every action governed", b.tagline()); // blank → default
        assertEquals("", b.kicker());
    }

    @Test
    void trimsSoAStraySpaceCannotSmuggleInADifferentName() {
        assertEquals("MantleKeep", new Brand("  MantleKeep  ", null, null, null).name());
    }
}
