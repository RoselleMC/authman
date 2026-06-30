package com.iroselle.authman.injector;

import java.lang.instrument.Instrumentation;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.ProtectionDomain;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.jar.JarEntry;
import java.util.jar.JarFile;
import java.util.jar.JarOutputStream;
import net.bytebuddy.agent.builder.AgentBuilder;
import net.bytebuddy.description.type.TypeDescription;
import net.bytebuddy.dynamic.DynamicType;
import net.bytebuddy.implementation.MethodDelegation;
import net.bytebuddy.matcher.ElementMatchers;
import net.bytebuddy.utility.JavaModule;

public final class AuthmanInjectorAgent {
    private static final String STRING_UTIL = "net.minecraft.util.StringUtil";
    private static final String PLAYER = "net.minecraft.world.entity.player.Player";
    private static final String POLICY_RESOURCE = "com/iroselle/authman/injector/AuthmanNamePolicy.class";

    private AuthmanInjectorAgent() {
    }

    public static void premain(String agentArgs, Instrumentation instrumentation) {
        install(agentArgs, instrumentation);
    }

    public static void agentmain(String agentArgs, Instrumentation instrumentation) {
        install(agentArgs, instrumentation);
    }

    private static void install(String agentArgs, Instrumentation instrumentation) {
        Map<String, String> options = parseOptions(agentArgs);
        boolean debug = Boolean.parseBoolean(options.getOrDefault("debug", "false"));
        appendPolicyToBootstrap(instrumentation);
        AuthmanNamePolicy.configure(debug);

        AgentBuilder agentBuilder = new AgentBuilder.Default()
            .with(AgentBuilder.RedefinitionStrategy.RETRANSFORMATION)
            .with(new Listener(debug))
            .type(ElementMatchers.named(STRING_UTIL))
            .transform(AuthmanInjectorAgent::transformStringUtil)
            .type(ElementMatchers.named(PLAYER))
            .transform(AuthmanInjectorAgent::transformPlayer);

        agentBuilder.installOn(instrumentation);
        log("installed; target classes=" + STRING_UTIL + ", " + PLAYER + "; debug=" + debug);
    }

    private static void appendPolicyToBootstrap(Instrumentation instrumentation) {
        try {
            Path folder = Files.createTempDirectory("authman-injector-bootstrap");
            File file = folder.resolve("authman-injector-bootstrap.jar").toFile();
            try (InputStream input = AuthmanInjectorAgent.class.getClassLoader().getResourceAsStream(POLICY_RESOURCE);
                 JarOutputStream output = new JarOutputStream(new FileOutputStream(file))) {
                if (input == null) {
                    throw new IllegalStateException("missing resource " + POLICY_RESOURCE);
                }
                output.putNextEntry(new JarEntry(POLICY_RESOURCE));
                input.transferTo(output);
                output.closeEntry();
            }
            instrumentation.appendToBootstrapClassLoaderSearch(new JarFile(file));
            file.deleteOnExit();
            folder.toFile().deleteOnExit();
            log("appended policy helper to bootstrap classpath: " + file.getAbsolutePath());
        } catch (Exception e) {
            log("failed to append policy helper to bootstrap classpath: " + e);
        }
    }

    private static DynamicType.Builder<?> transformStringUtil(
        DynamicType.Builder<?> builder,
        TypeDescription typeDescription,
        ClassLoader classLoader,
        JavaModule module,
        ProtectionDomain protectionDomain
    ) {
        return builder.method(
            ElementMatchers.isStatic()
                .and(ElementMatchers.takesArguments(String.class))
                .and(ElementMatchers.returns(boolean.class))
                .and(ElementMatchers.named("isValidPlayerName")
                    .or(ElementMatchers.named("isReasonablePlayerName")))
        ).intercept(MethodDelegation.to(AuthmanNamePolicy.class));
    }

    private static DynamicType.Builder<?> transformPlayer(
        DynamicType.Builder<?> builder,
        TypeDescription typeDescription,
        ClassLoader classLoader,
        JavaModule module,
        ProtectionDomain protectionDomain
    ) {
        return builder.method(
            ElementMatchers.isStatic()
                .and(ElementMatchers.takesArguments(String.class))
                .and(ElementMatchers.returns(boolean.class))
                .and(ElementMatchers.named("isValidUsername"))
        ).intercept(MethodDelegation.to(AuthmanNamePolicy.class));
    }

    private static Map<String, String> parseOptions(String agentArgs) {
        Map<String, String> options = new LinkedHashMap<>();
        if (agentArgs == null || agentArgs.isBlank()) {
            return options;
        }
        for (String part : agentArgs.split(",")) {
            String trimmed = part.trim();
            if (trimmed.isEmpty()) {
                continue;
            }
            int eq = trimmed.indexOf('=');
            if (eq < 0) {
                options.put(trimmed, "true");
            } else {
                options.put(trimmed.substring(0, eq).trim(), trimmed.substring(eq + 1).trim());
            }
        }
        return options;
    }

    private static void log(String message) {
        System.err.println("[AuthmanInjector] " + message);
    }

    private static final class Listener extends AgentBuilder.Listener.Adapter {
        private final boolean debug;

        private Listener(boolean debug) {
            this.debug = debug;
        }

        @Override
        public void onTransformation(
            TypeDescription typeDescription,
            ClassLoader classLoader,
            JavaModule module,
            boolean loaded,
            DynamicType dynamicType
        ) {
            log("transformed " + typeDescription.getName() + " loaded=" + loaded);
        }

        @Override
        public void onError(
            String typeName,
            ClassLoader classLoader,
            JavaModule module,
            boolean loaded,
            Throwable throwable
        ) {
            log("failed to transform " + typeName + ": " + throwable);
            if (debug) {
                throwable.printStackTrace(System.err);
            }
        }
    }
}
