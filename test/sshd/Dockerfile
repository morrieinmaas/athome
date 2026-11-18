# An sshd the machines drill logs in to: user dev, keys only, no root login.
# The first authorized key arrives at start through AUTHORIZED_KEY, so no
# secret is baked into the image.
FROM alpine:3.20
RUN apk add --no-cache openssh-server \
    && adduser -D -s /bin/sh dev \
    && echo "dev:$(head -c 24 /dev/urandom | base64)" | chpasswd \
    && ssh-keygen -A \
    && printf '%s\n' \
        'PasswordAuthentication no' \
        'KbdInteractiveAuthentication no' \
        'PermitRootLogin no' \
        'PubkeyAuthentication yes' \
        'AllowUsers dev' \
        > /etc/ssh/sshd_config.d/10-drill.conf \
    && grep -q '^Include' /etc/ssh/sshd_config || echo 'Include /etc/ssh/sshd_config.d/*.conf' >> /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
