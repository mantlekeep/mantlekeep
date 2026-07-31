package dev.mantlekeep.springboot.webflux.config;

import dev.mantlekeep.springboot.brand.Brand;
import dev.mantlekeep.springboot.door.DoorClient;
import dev.mantlekeep.springboot.door.DoorProperties;
import dev.mantlekeep.springboot.webflux.door.WebClientDoorClient;
import dev.mantlekeep.springboot.webflux.error.DoorExceptionHandler;
import dev.mantlekeep.springboot.webflux.intent.MantlekeepIntentAspect;
import io.netty.channel.ChannelOption;
import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.http.HttpHeaders;
import org.springframework.http.client.reactive.ReactorClientHttpConnector;
import org.springframework.web.reactive.function.client.WebClient;
import reactor.netty.http.client.HttpClient;

/**
 * Auto-configuration for the reactive door client. From {@code mantlekeep.door.*} it wires a
 * timeout-bounded, dedicated {@link WebClient} and a reactive {@link DoorClient}. Every
 * bean is {@link ConditionalOnMissingBean}, so an application can override any piece.
 */
@AutoConfiguration
@EnableConfigurationProperties({MantlekeepDoorProperties.class, MantlekeepBrandProperties.class})
public class MantlekeepAutoConfiguration {

    @Bean
    @ConditionalOnMissingBean
    public DoorProperties mantleDoorProperties(MantlekeepDoorProperties props) {
        return new DoorProperties(props.baseUrl(), props.governPath(), props.bearerToken(),
                props.serviceUser(), props.serviceUserHeader(), props.onBehalfOfHeader(),
                props.connectTimeout(), props.responseTimeout());
    }

    /**
     * The product's brand, for anything that shows a name to an operator (a future
     * {@code /api/brand}, a UI header). The startup banner reads the same {@code mantlekeep.brand.*}
     * config directly from the Environment, so it wears the brand even before this bean exists.
     */
    @Bean
    @ConditionalOnMissingBean
    public Brand mantleBrand(MantlekeepBrandProperties props) {
        return new Brand(props.name(), props.mark(), props.tagline(), props.kicker());
    }

    /** A dedicated WebClient for the door — isolated from the app's own WebClient(s). */
    @Bean
    @ConditionalOnMissingBean(name = "mantleDoorWebClient")
    public WebClient mantleDoorWebClient(DoorProperties properties) {
        HttpClient httpClient = HttpClient.create()
                .option(ChannelOption.CONNECT_TIMEOUT_MILLIS,
                        Math.toIntExact(properties.connectTimeout().toMillis()))
                .responseTimeout(properties.responseTimeout());

        WebClient.Builder builder = WebClient.builder()
                .baseUrl(properties.baseUrl())
                .clientConnector(new ReactorClientHttpConnector(httpClient));
        if (!properties.bearerToken().isBlank()) {
            builder = builder.defaultHeader(HttpHeaders.AUTHORIZATION,
                    "Bearer " + properties.bearerToken());
        }
        return builder.build();
    }

    @Bean
    @ConditionalOnMissingBean
    public DoorClient doorClient(WebClient mantleDoorWebClient, DoorProperties properties) {
        return new WebClientDoorClient(mantleDoorWebClient, properties);
    }

    /** The aspect that governs {@code @MantlekeepIntent} methods through the door. */
    @Bean
    @ConditionalOnMissingBean
    public MantlekeepIntentAspect mantleIntentAspect(DoorClient doorClient) {
        return new MantlekeepIntentAspect(doorClient);
    }

    /**
     * Maps a door denial to a clean {@code 403} (with reasons) and a transport failure to
     * {@code 503}, so a governed refusal is never a 500 — for every product on the starter.
     */
    @Bean
    @ConditionalOnMissingBean
    public DoorExceptionHandler mantleDoorExceptionHandler() {
        return new DoorExceptionHandler();
    }
}
