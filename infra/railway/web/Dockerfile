FROM nginx:1.27-alpine
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY railway-entrypoint.sh /usr/local/bin/railway-entrypoint.sh
RUN chmod +x /usr/local/bin/railway-entrypoint.sh
COPY site/ /usr/share/nginx/html/
ENV NGINX_ENVSUBST_FILTER="^(PORT|NGINX_RESOLVER)$"
ENTRYPOINT ["/usr/local/bin/railway-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
