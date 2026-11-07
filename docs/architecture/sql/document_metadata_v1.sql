BEGIN;
SET LOCAL search_path=akos_withdraw_v1,pg_catalog;

CREATE TABLE document_metadata(
 space_id platform_id NOT NULL,
 kb_id platform_id NOT NULL,
 document_id platform_id NOT NULL,
 title text NOT NULL CHECK(char_length(title) BETWEEN 1 AND 10000),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(space_id,kb_id,document_id),
 FOREIGN KEY(space_id,kb_id,document_id) REFERENCES documents(space_id,kb_id,id)
);

CREATE TRIGGER document_metadata_immutable BEFORE UPDATE OR DELETE ON document_metadata FOR EACH ROW EXECUTE FUNCTION immutable_record();

COMMIT;
