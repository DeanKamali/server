/*
  Copyright (c) 2025 MariaDB

  This program is free software; you can redistribute it and/or modify
  it under the terms of the GNU General Public License as published by
  the Free Software Foundation; version 2 of the License.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
  See the GNU General Public License for more details.

  You should have received a copy of the GNU General Public License along
  with this program; if not, write to the Free Software Foundation, Inc.,
  51 Franklin St, Fifth Floor, Boston, MA 02110-1335 USA.
*/

#include "rpl_info_file.hh"

struct RelayLogInfoFile: InfoFile
{
  /**
    `@@relay_log_info_file` fields in SHOW SLAVE STATUS order
    @{
  */
  StringField<> relay_log_file;
  IntField<my_off_t> relay_log_pos;
  /// Relay_Master_Log_File (of the event *group*)
  StringField<> read_master_log_file;
  /// Exec_Master_Log_Pos (of the event *group*)
  IntField<my_off_t> read_master_log_pos;
  /// SQL_Delay
  IntField<uint32_t> sql_delay;
  /// }@

  inline static const std::initializer_list<mem_fn> FIELDS_LIST= {
    &RelayLogInfoFile::relay_log_file,
    &RelayLogInfoFile::relay_log_pos,
    &RelayLogInfoFile::read_master_log_file,
    &RelayLogInfoFile::read_master_log_pos,
    &RelayLogInfoFile::sql_delay
  };
};
