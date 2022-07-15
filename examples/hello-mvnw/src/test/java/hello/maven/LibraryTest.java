package hello.maven;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class LibraryTest {
    @Test void runMavenWrapperTests() {
        String env = System.getenv("RUNNER_ENV_TEST");
        assertTrue(Boolean.parseBoolean(env), "RUNNER_ENV_TEST env should be true");
    }
}
