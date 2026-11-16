# syntax=docker/dockerfile:1
#
# web.Dockerfile - nginx serving the built browser client
#
# The client is built on the publishing machine (publish.sh runs
# `pnpm --filter @sro/client-next build`), because the build stamps the git
# revision the release watch compares, and a build context has no .git.
# The built tree arrives as a second context:
#   --build-context dist=<apps/client-next/temp/artifacts/dist>
# The game assets are not in the image: rollout.sh mounts the synced
# release at /srv/game (it holds the assets/ tree).

FROM nginx:1.27-alpine
RUN rm -f /etc/nginx/conf.d/default.conf
COPY deploy/docker/nginx.conf /etc/nginx/conf.d/silkroad.conf
COPY --from=dist . /usr/share/nginx/html/
