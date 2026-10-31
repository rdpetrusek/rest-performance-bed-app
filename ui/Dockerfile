# The ReST Bed dashboard's frontend: the Angular build served by nginx, which
# also proxies /api to the API service (API_UPSTREAM, default api:8000). See
# ui/compose.yaml for how this is run.

# The build output is static files, so build once on the build machine's own
# platform whatever the target architecture.
FROM --platform=$BUILDPLATFORM node:22-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
# The image runs envsubst over /etc/nginx/templates/*.template at start.
ENV API_UPSTREAM=api:8000
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /src/dist/ui/browser/ /usr/share/nginx/html/
EXPOSE 80
