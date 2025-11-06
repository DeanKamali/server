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

#ifndef RPL_INFO_FILE_HH
#define RPL_INFO_FILE_HH

#include <charconv>    // std::from/to_chars and other helpers
#include <functional>  // superclass of InfoFile::mem_fn
#include <my_sys.h>    // IO_CACHE
#include <my_global.h> // FN_REFLEN
#include "slave.h"     // init_strvar_from_file


namespace IntIOCache
{
  /** Number of fully-utilized decimal digits plus
    * the partially-utilized digit (e.g., the 2's place in "2147483647")
    * The sign (:
  */
  template<typename I> static constexpr size_t BUF_SIZE=
    std::numeric_limits<I>::digits10 + 2;
  static constexpr auto ERRC_OK= std::errc();

  /**
    @ref IO_CACHE (reading one line with the `\n`) version of std::from_chars(),
    zero and 64-bit capable version of `init_intvar_from_file()`
    @tparam I integer type
    @return `false` if the line has parsed successfully or `true` if error
  */
  template<typename I> static bool from_chars(IO_CACHE *file, I &value)
  {
    /**
      +2 for the terminating `\n\0` (They are ignored by
      std::from_chars(), but my_b_gets() includes them.)
    */
    char buf[BUF_SIZE<I> + 2];
    /// includes the `\n` but excludes the `\0`
    size_t size= my_b_gets(file, buf, sizeof(buf));
    // SFINAE if `I` is not a numeric type
    return !size || std::from_chars(buf, &buf[size], value).ec != ERRC_OK;
  }
  /**
    Convenience overload of from_chars(IO_CACHE *, I &) for `operator=` types
    @tparam I inner integer type
    @tparam T wrapper type
  */
  template<typename I, class T> static bool from_chars(IO_CACHE *file, T *self)
  {
    I value;
    if (from_chars(file, value))
      return true;
    (*self)= value;
    return false;
  }

  /**
    @ref IO_CACHE (writing *without* a `\n`) version of std::to_chars()
    @tparam I (inner) integer type
  */
  template<typename I> static void to_chars(IO_CACHE *file, I value)
  {
    /**
      my_b_printf() uses a buffer too,
      so we might as well save on format parsing and buffer resizing
    */
    char buf[BUF_SIZE<I>];
    std::to_chars_result result= std::to_chars(buf, &buf[sizeof(buf)], value);
    DBUG_ASSERT(result.ec == ERRC_OK);
    my_b_write(file, (const uchar *)buf, result.ptr - buf);
  }
};


/**
  This common superclass of @ref MasterInfoFile and
  @ref RelayLogInfoFile provides them common code for saving
  and loading fields in their MySQL line-based sections.
  As only the @ref MasterInfoFile has a MariaDB `key=value`
  section with a mix of explicit and `DEFAULT`-able fields,
  code for those are in @ref MasterInfoFile instead.

  Each field is an instance of an implementation
  of the @ref InfoFile::Persistent interface.
  C++ templates enables code reuse for those implementation structs, but
  templates are not suitable for the conventional header/implementation split.
  Thus, this and derived files are header-only units (methods are `inline`).
  Other files may include these files directly,
  though headers should include this set under their `#include` guards.
  [C++20 modules](https://en.cppreference.com/w/cpp/language/modules.html)
  can supercede headers and their `#include` guards.
*/
struct InfoFile
{
  IO_CACHE file;


  /// Persistence interface for an unspecified item
  struct Persistent
  {
    virtual ~Persistent()= default;
    // for save_to_file()
    virtual bool is_default() { return false; }
    /// @return `true` if the item is mandatory and couldn't provide a default
    virtual bool set_default() { return true; }
    /** set the value by reading a line from the IO and consume the `\n`
      @return `false` if the line has parsed successfully or `true` if error
      @post is_default() is `false`
    */
    virtual bool load_from(IO_CACHE *file)= 0;
    /** write the *effective* value to the IO **without** a `\n`
      (The caller will separately determine how
      to represent using the default value.)
    */
    virtual void save_to(IO_CACHE *file)= 0;
  };

  /** Integer Field
    @tparam I signed or unsigned integer type
    @see MasterInfoFile::OptionalIntField
      version with `DEFAULT` (not a subclass)
  */
  template<typename I> struct IntField: Persistent
  {
    I value;
    operator I() { return value; }
    auto &operator=(I value)
    {
      this->value= value;
      return *this;
    }
    virtual bool load_from(IO_CACHE *file) override
    { return IntIOCache::from_chars(file, value); }
    virtual void save_to(IO_CACHE *file) override
    { return IntIOCache::to_chars(file, value); }
  };

  /// Null-Terminated String (usually file name) Field
  template<size_t N= FN_REFLEN> struct StringField: Persistent
  {
    char buf[N];
    virtual operator const char *() { return buf; }
    /// @param other not `nullptr`
    auto &operator=(const char *other)
    {
      strmake_buf(this->buf, other);
      return *this;
    }
    virtual bool load_from(IO_CACHE *file) override
    { return init_strvar_from_file(buf, N, file, nullptr); }
    virtual void save_to(IO_CACHE *file) override
    {
      const char *buf= *this;
      my_b_write(file, (const uchar *)buf, strlen(buf));
    }
  };


protected:

  /**
    std::mem_fn()-like nullable replacement for
    [member pointer upcasting](https://wg21.link/P0149R3)
  */
  struct mem_fn: std::function<Persistent &(InfoFile *self)>
  {
    /// Null Constructor
    mem_fn(nullptr_t null= nullptr):
      std::function<Persistent &(InfoFile *)>(null) {}
    /** Non-Null Constructor
      @tparam T CRTP subclass of InfoFile
      @tparam M @ref Persistent subclass of the member
      @param pm member pointer
    */
    template<class T, typename M> mem_fn(M T::* pm):
      std::function<Persistent &(InfoFile *)>(
        [pm](InfoFile *self) -> Persistent &
        { return self->*static_cast<M InfoFile::*>(pm); }
      ) {}
  };
};

#endif
