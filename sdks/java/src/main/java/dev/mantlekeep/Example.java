package dev.mantlekeep;

/** Run against a live core: `mantlekeep serve`, then `java dev/mantle/Example.java`. */
public class Example {
    public static void main(String[] args) {
        var mantlekeep = MantlekeepClient.connect("http://localhost:8080").login("lead-bob"); // Operator

        var dev = mantlekeep.govern(Intent.action("job.promote").env("DEV").goal("ship the release"));
        System.out.println("promote DEV  → allowed=" + dev.allowed() + " token=" + dev.token());

        var prod = mantlekeep.govern(Intent.action("job.promote").env("PROD").goal("ship the release"));
        System.out.println("promote PROD → allowed=" + prod.allowed() + " reason=" + prod.reason());

        // one-line governance guard — throws if denied:
        try {
            mantlekeep.govern(Intent.action("job.promote").env("PROD").goal("ship")).require();
        } catch (MantlekeepDeniedException e) {
            System.out.println("guard blocked the action: " + e.getMessage());
        }
    }
}
