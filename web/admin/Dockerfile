# syntax=docker/dockerfile:1.7
# ──────────── build stage ────────────
FROM node:20-alpine AS build
WORKDIR /workspace

# Copy workspace manifests first for better layer caching.
COPY package.json package-lock.json* .npmrc tsconfig.base.json ./
COPY shared/package.json shared/tsconfig.json shared/
COPY admin/package.json admin/tsconfig.json admin/vite.config.ts admin/env.d.ts admin/index.html admin/
# e2e/player are not needed but their package.json must exist to satisfy workspaces.
COPY player/package.json player/
COPY e2e/package.json e2e/

RUN npm install --no-fund --no-audit

# Copy source.
COPY shared/src ./shared/src
COPY admin/src ./admin/src

RUN npm run build -w @authman/admin

# ──────────── runtime stage ────────────
FROM nginx:1.27-alpine AS runtime

RUN rm /etc/nginx/conf.d/default.conf
COPY admin/nginx.conf /etc/nginx/conf.d/default.conf

COPY --from=build /workspace/admin/dist /usr/share/nginx/html

COPY admin/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080
ENV AUTHMAN_API_BASE=/api \
    AUTHMAN_DEFAULT_LOCALE=en

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
