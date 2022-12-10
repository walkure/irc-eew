#!/usr/bin/env perl

# show list of saved EEW data
# walkure at kmc.gr.jp

use strict;
use warnings;
use File::Slurp qw(read_file);
use CGI::Fast;
use utf8;
use lib './lib/';
use Earthquake::EEW::Decoder;
use Data::Dumper;

my $ddir =      defined $ENV{EEW_DATA_DIR}   ? $ENV{EEW_DATA_DIR}   : '/eewlog/';
my $path_base = defined $ENV{EEW_PATH_BASE}  ? $ENV{EEW_PATH_BASE}  : './';
my $viewer =    defined $ENV{EEW_VIEWER}     ? $ENV{EEW_VIEWER}     :'eew-show' ;

binmode STDOUT, ":utf8";
#my $query = new CGI;

# https://github.com/perl-catalyst/FCGI/commit/7369e6b96a59b425f5b44bdf52a95387baa0e782
if(defined *FCGI::Stream::PRINT){
	my $fcgi_print = \&FCGI::Stream::PRINT;
	undef *FCGI::Stream::PRINT;
	*FCGI::Stream::PRINT = sub {
		my $stream = shift;
		my @args = map {my $i = $_ ; utf8::encode($i); $i} @_;

		&$fcgi_print($stream,@args);
	};
}

while(my $query = new CGI::Fast){
	main($query);
}

sub main
{
	my $q = shift;
	my $year = $q->param('year') ;
	my $month = $q->param('month');
	my $day = $q->param('day');

	$year = saturate($year, 2000, 3000);
	$month = saturate($month, 1, 12);
	$day = saturate($day, 1, 31);

	print << "_HTML_";
Content-Type:text/html;charset=utf-8

<html><head><title>list of EEW Data</title>
<meta name="robots" content="noindex,nofollow,noarchive" />
</head>
<body>
	<ul>
_HTML_

	unless( -e "$ddir/$year" ){
		opendir(my $dh,$ddir) or die "opendir($ddir):$!";
		foreach my $d (sort readdir($dh)){
			next unless $d =~ /^\d+/;
			print qq|<li><a href="$path_base?year=$d">${d}年</a></li>\n|;
		}
		closedir($dh);
		print << "_HTML_";
</body></html>
_HTML_
		return;
	}
	unless( -e sprintf('%s/%04d/%02d',$ddir,$year,$month)){
		opendir(my $dh,"$ddir/$year") or die "opendir($ddir):$!";
		my $prefix =  qq|<a href="$path_base?year=$year">${year}</a>年|;
		foreach my $d (sort readdir($dh)){
			next unless $d =~ /^\d+/;
			$d += 0;
			print qq|<li>$prefix<a href="$path_base?year=$year&month=$d">${d}</a>月</li>\n|;
		}
		closedir($dh);
		print << "_HTML_";
</body></html>
_HTML_
		return;
	}
	unless( -e sprintf('%s/%04d/%02d/%02d',$ddir,$year,$month,$day)){
		opendir(my $dh,sprintf('%s/%04d/%02d',$ddir,$year,$month)) or die "opendir($ddir):$!";
		my $prefix = qq|<a href="$path_base?year=$year">${year}</a>年<a href="$path_base?year=$year&month=$month">${month}</a>月|;
		foreach my $d (sort readdir($dh)){
			next unless $d =~ /^\d+/;
			$d += 0;
			print qq|<li>$prefix<a href="$path_base?year=$year&month=$month&day=$d">${d}</a>日</li>\n|;
		}
		closedir($dh);
		print << "_HTML_";
</body></html>
_HTML_
		return;
	}

	opendir(my $dh,sprintf('%s/%04d/%02d/%02d',$ddir,$year,$month,$day)) or die "opendir($ddir):$!";
	my $prefix = qq|<a href="$path_base?year=$year">$year</a>年<a href="$path_base?year=$year&month=$month">$month</a>月${day}日|;
	foreach my $d (sort readdir($dh)){
		next unless $d =~ /^\d+\.\d+/;

		my $path = sprintf('%s/%04d/%02d/%02d/%s',$ddir,$year,$month,$day,$d);
		my $summary = get_eew_summary($path);
		print qq|<li>$prefix <a href="$path_base$viewer?name=$d">■</a> $summary</li>\n|;
	}
	closedir($dh);
	print << "_HTML_";
</body></html>
_HTML_

}
sub saturate{
	my($value,$min,$max) = @_;

	return 0 unless defined $value;
	return $max if $value > $max;
	return $min if $value < $min;
	$value;
}

sub get_eew_summary
{
	my $path = shift;
	my $eew = Earthquake::EEW::Decoder->new();
	my $data = read_file($path);
	my $d = $eew->read_data($data, { binmode => ':encoding(sjis)' });

	my @wd = $d->{warn_time} =~ /\d\d/og;
	my @ed = $d->{eq_time}   =~ /\d\d/og;
	
	my $warnmsg = "$wd[3]:$wd[4]:$wd[5]";
	my $eqedmsg = "$ed[3]:$ed[4]:$ed[5]";

	my $times = '第'.sprintf '%02d報%s',
		$d->{warn_num},
		$d->{NCN_type} >=8 ? '(最終)' : '&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;';

	my $msg;
	if($d->{'msg_type_code'} == 10){
        $msg = $warnmsg.$times.' ('.$eqedmsg.'発生) 取り消し';
	}else{
        my $center = sprintf('震央:N%.01f/E%0.01f(%s)深さ%dkm',$d->{'center_lat'},$d->{'center_lng'},$d->{'center_name'},$d->{'center_depth'});
        my $magnitude = sprintf(' 最大:M%.01f 震度%s',$d->{'magnitude'},$d->{'shindo'});
        $msg = $warnmsg.$times.' ('.$eqedmsg.'発生)'.$center.$magnitude;
	}
	
	$msg.= ' (地域予想震度情報あり)' if defined $d->{EBI};

	$msg;
}
