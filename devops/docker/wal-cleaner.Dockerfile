FROM postgres:12

COPY devops/scripts/walcleaner.sh /bin/walcleaner.sh

ENTRYPOINT [ "/bin/walcleaner.sh" ]