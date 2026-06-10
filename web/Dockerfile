FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY index.html signup.html login.html post.html profile.html users.html search.html style.css /usr/share/nginx/html/
COPY users/ /usr/share/nginx/html/users/
EXPOSE 80
