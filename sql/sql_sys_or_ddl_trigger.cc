#include "mariadb.h"                          /* NO_EMBEDDED_ACCESS_CHECKS */

#include <m_ctype.h>
#include <mysqld_error.h>

#include "lex_string.h"
#include "table.h"

#include "event_db_repository.h"  // enum_events_table_field
#include "event_parse_data.h"     // Event_parse_data

#include "key.h"                  // key_copy
#include "lock.h"                 // lock_object_name
#include "sp_head.h"              // sp_head
#include "sql_parse.h"            // sp_process_definer
#include "sql_sys_or_ddl_trigger.h"
#include "sql_trigger.h"
#include "strfunc.h"              //set_to_string

/**
  Raise the error ER_TRG_ALREADY_EXISTS
*/

static void report_error(uint error_num, sp_name *spname)
{
  /*
    Report error in case there is a trigger on DML event with
    the same name as the system trigger we are going to create
  */
  char trigname_buff[FN_REFLEN];

  strxnmov(trigname_buff, sizeof(trigname_buff) - 1,
           spname->m_db.str, ".",
           spname->m_name.str, NullS);
  my_error(error_num, MYF(0), trigname_buff);
}


/**
  Check whether there is a trigger specified name on a DML event

  @return true and set an error in DA in case there is a trigger
          with supplied name on DML event, else return false
*/

static bool check_dml_trigger_exist(sp_name *spname)
{
  char trn_path_buff[FN_REFLEN];
  LEX_CSTRING trn_path= { trn_path_buff, 0 };

  build_trn_path(spname, (LEX_STRING*) &trn_path);

  if (!check_trn_exists(&trn_path))
  {
    /*
      Report error in case there is a trigger on DML event with
      the same name as the system trigger we are going to create
    */
    report_error(ER_TRG_ALREADY_EXISTS, spname);
    return true;
  }

  return false;
}


/**
  Search a system or ddl trigger by its name in the table mysql.event.

  @return false in case there is no trigger with specified name,
          else return true
*/

static bool find_sys_trigger_by_name(TABLE *event_table, sp_name *spname)
{
  event_table->field[ET_FIELD_DB]->store(spname->m_db.str,
                                         spname->m_db.length, &my_charset_bin);
  event_table->field[ET_FIELD_NAME]->store(spname->m_name.str,
                                           spname->m_name.length,
                                           &my_charset_bin);

  uchar key[MAX_KEY_LENGTH];
  key_copy(key, event_table->record[0], event_table->key_info,
           event_table->key_info->key_length);

  int ret= event_table->file->ha_index_read_idx_map(event_table->record[0], 0,
                                                    key, HA_WHOLE_KEY,
                                                    HA_READ_KEY_EXACT);
  /*
    ret != 0 in case 'row not found'; ret == 0 if 'row found'
  */
  return !ret;
}

static bool store_trigger_metadata(THD *thd, LEX *lex, TABLE *event_table,
                                   sp_head *sphead,
                                   const st_trg_chistics &trg_chistics)
{
  restore_record(event_table, s->default_values);

  if (sphead->m_body.length > event_table->field[ET_FIELD_BODY]->field_length)
  {
    my_error(ER_TOO_LONG_BODY, MYF(0), sphead->m_name.str);

    return true;
  }

  Field **fields= event_table->field;
  int ret;

  char definer_buf[USER_HOST_BUFF_SIZE];
  LEX_CSTRING definer;
  thd->lex->definer->set_lex_string(&definer, definer_buf);

  if (fields[ET_FIELD_DEFINER]->store(definer.str, definer.length,
                                      system_charset_info))
  {
    my_error(ER_EVENT_DATA_TOO_LONG, MYF(0),
             fields[ET_FIELD_DEFINER]->field_name.str);
    return true;
  }

  if (fields[ET_FIELD_DB]->store(sphead->m_db.str,
                                 sphead->m_db.length,
                                 system_charset_info))
   {
     my_error(ER_EVENT_DATA_TOO_LONG, MYF(0),
              fields[ET_FIELD_DB]->field_name.str);
     return true;
   }

  if (fields[ET_FIELD_NAME]->store(sphead->m_name.str,
                                   sphead->m_name.length,
                                   system_charset_info))
   {
     my_error(ER_EVENT_DATA_TOO_LONG, MYF(0),
              fields[ET_FIELD_DB]->field_name.str);
     return true;
   }

  ret= fields[ET_FIELD_ON_COMPLETION]->store(
    (longlong)Event_parse_data::ON_COMPLETION_DEFAULT, true);
  if (ret)
  {
    my_error(ER_EVENT_STORE_FAILED, MYF(0),
             fields[ET_FIELD_ON_COMPLETION]->field_name.str, ret);
    return true;
  }

  ret= fields[ET_FIELD_ORIGINATOR]->store(
    (longlong)global_system_variables.server_id, true);
  if (ret)
  {
    my_error(ER_EVENT_STORE_FAILED, MYF(0),
             fields[ET_FIELD_ORIGINATOR]->field_name.str, ret);
    return true;
  }

  ret= fields[ET_FIELD_CREATED]->set_time();
  if (ret)
  {
    my_error(ER_EVENT_STORE_FAILED, MYF(0),
             fields[ET_FIELD_CREATED]->field_name.str, ret);
    return true;
  }

  if (fields[ET_FIELD_BODY]->store(sphead->m_body.str,
                                   sphead->m_body.length,
                                   system_charset_info))
  {
    my_error(ER_EVENT_STORE_FAILED, MYF(0),
             fields[ET_FIELD_BODY]->field_name.str, ret);
    return true;
  }

  /*
    trg_chistics.events has meaningful bits for every trigger events,
    that is for DML, DDL, system events. The table mysql.event declares
    the column `kind` as a set with the following values
      `kind` set('SCHEDULE','STARTUP','SHUTDOWN','LOGON','LOGOFF','DDL')
    Since the first value is special value `SCHEDULE`, move events value
    one bit left.
  */
  longlong trg_events= (trg_chistics.events >> 3);
  ret= fields[ET_FIELD_KIND]->store((trg_events << 1), true);
  if (ret)
  {
    my_error(ER_EVENT_STORE_FAILED, MYF(0),
             fields[ET_FIELD_KIND]->field_name.str, ret);
    return true;
  }

  ret= fields[ET_FIELD_WHEN]->store((longlong)trg_chistics.action_time + 1,
                                    true);
  if (ret)
  {
    my_error(ER_EVENT_STORE_FAILED, MYF(0),
             fields[ET_FIELD_WHEN]->field_name.str, ret);
    return true;
  }
  fields[ET_FIELD_WHEN]->set_notnull();

  ret= event_table->file->ha_write_row(event_table->record[0]);
  if (ret)
  {
    event_table->file->print_error(ret, MYF(0));
    return true;
  }

  return false;
}

// Transaction_Resources_Guard
class Transaction_Resources_Guard
{
public:
  Transaction_Resources_Guard(THD *thd, sql_mode_t saved_mode)
  : m_thd{thd}, m_mdl_savepoint{thd->mdl_context.mdl_savepoint()},
    m_saved_mode{saved_mode}
  {}
  ~Transaction_Resources_Guard()
  {
    m_thd->commit_whole_transaction_and_close_tables();
    m_thd->mdl_context.rollback_to_savepoint(m_mdl_savepoint);
    m_thd->variables.sql_mode= m_saved_mode;
  }
private:
  THD *m_thd;
  MDL_savepoint m_mdl_savepoint;
  sql_mode_t m_saved_mode;
};

bool mysql_create_sys_trigger(THD *thd)
{
  if (!thd->lex->spname->m_db.length)
  {
    my_error(ER_NO_DB_ERROR, MYF(0));
    return true;
  }

  /*
    We don't allow creating triggers on tables in the 'mysql' schema
  */
  if (thd->lex->spname->m_db.streq(MYSQL_SCHEMA_NAME))
  {
    my_error(ER_NO_TRIGGERS_ON_SYSTEM_SCHEMA, MYF(0));
    return true;
  }

  if (thd->lex->trg_chistics.action_time == TRG_ACTION_BEFORE &&
      (sys_trg2bit(TRG_EVENT_STARTUP) & thd->lex->trg_chistics.events))
  {
    my_error(ER_SYS_TRG_SEMANTIC_ERROR, MYF(0), thd->lex->spname->m_db.str,
             thd->lex->spname->m_name.str, "BEFORE", "STARTUP");
    return true;
  }

  if (thd->lex->trg_chistics.action_time == TRG_ACTION_AFTER &&
      (sys_trg2bit(TRG_EVENT_SHUTDOWN) & thd->lex->trg_chistics.events))
  {
    my_error(ER_SYS_TRG_SEMANTIC_ERROR, MYF(0), thd->lex->spname->m_db.str,
             thd->lex->spname->m_name.str, "AFTER", "SHUTDOWN");
    return true;
  }

  if (sp_process_definer(thd))
    return true;

  /*
    Since the table mysql.event is used both for storing meta data about
    events and system/ddl triggers, use the MDL_key::EVENT namespace
    for acquiring the mdl lock
  */
  if (lock_object_name(thd, MDL_key::EVENT, thd->lex->spname->m_db,
                       thd->lex->spname->m_name))
    return true;

  if (check_dml_trigger_exist(thd->lex->spname))
    return true;

  /* Reset sql_mode during data dictionary operations. */
  sql_mode_t saved_mode= thd->variables.sql_mode;
  thd->variables.sql_mode= 0;

  TABLE *event_table;
  if (Event_db_repository::open_event_table(thd, TL_WRITE, &event_table))
  {
    thd->variables.sql_mode= saved_mode;

    return true;
  }
  /*
    Activate the guard to release mdl lock to the savepoint and commit
    transaction on any return path from this function.
  */
  Transaction_Resources_Guard mdl_savepoint_guard{thd, saved_mode};

  if (find_sys_trigger_by_name(event_table, thd->lex->spname))
  {
    if (thd->lex->create_info.if_not_exists())
      return false;

    report_error(ER_TRG_ALREADY_EXISTS, thd->lex->spname);
    return true;
  }

  if (store_trigger_metadata(thd, thd->lex, event_table, thd->lex->sphead,
                             thd->lex->trg_chistics))
    return true;

  my_ok(thd);
  return false;
}

bool mysql_drop_sys_or_ddl_trigger(THD *thd, bool *no_ddl_trigger_found)
{
  MDL_request mdl_request;

  /*
    Note that once we will have check for TRIGGER privilege in place we won't
    need second part of condition below, since check_access() function also
    checks that db is specified.
  */
  if (!thd->lex->spname->m_db.length)
  {
    my_error(ER_NO_DB_ERROR, MYF(0));
    return true;
  }

  *no_ddl_trigger_found= false;

  /* Protect against concurrent create/drop */
  if (lock_object_name(thd, MDL_key::TRIGGER, thd->lex->spname->m_db,
                       thd->lex->spname->m_name))
    return true;

  /* Reset sql_mode during data dictionary operations. */
  sql_mode_t saved_mode= thd->variables.sql_mode;
  thd->variables.sql_mode= 0;

  TABLE *event_table;
  if (Event_db_repository::open_event_table(thd, TL_WRITE, &event_table))
    return true;

  Transaction_Resources_Guard mdl_savepoint_guard{thd, saved_mode};

  if (!find_sys_trigger_by_name(event_table, thd->lex->spname))
  {
    /*
      The use case 'trigger not found' is handled at the function
      mysql_create_or_drop_trigger() if there is no a DML trigger
      with specified name
    */
    *no_ddl_trigger_found= true;
    return false;
  }

  int ret= event_table->file->ha_delete_row(event_table->record[0]);
  if (ret)
    event_table->file->print_error(ret, MYF(0));
  else
    my_ok(thd);

  return ret;
}

Sys_trigger *
get_trigger_by_type(THD *thd, trg_sys_event_type trg_type)
{
  return nullptr;
}

bool Sys_trigger::execute(THD *thd)
{
  return false;
}
